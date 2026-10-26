//go:build linux || darwin

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// echoMux answers status and counts requests that carried a password.
func echoMux(passwords *atomic.Int32) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vault/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"locked":true,"addr":%q}`, serverHost())
	})
	mux.HandleFunc("POST /vault/unlock", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["password"] != "" {
			passwords.Add(1)
		}
		w.Write([]byte(`{"token":"t"}`))
	})
	return mux
}

func TestCredentialRequest_OwnBinary(t *testing.T) {
	var got atomic.Int32
	serveOnSocket(t, serveHandler(t, echoMux(&got)))
	if err := checkVaultServer(); err != nil {
		t.Fatal(err)
	}
	resp, err := credentialRequest("POST", "/vault/unlock", map[string]string{"password": "pw"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || got.Load() != 1 {
		t.Fatalf("status %d, passwords received %d", resp.StatusCode, got.Load())
	}
}

// With nothing on the vault socket, nothing is sent, even if some server
// answers on the TCP port.
func TestCredentialRequest_NoServer(t *testing.T) {
	shortVaultDir(t)
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:1") // nothing listens there
	if err := checkVaultServer(); err != errNoVaultSocket {
		t.Fatalf("err = %v, want %v", err, errNoVaultSocket)
	}
	// A server on the TCP port without this vault's socket: an older
	// pvault, or an impostor. Either way the message says to restart.
	var got atomic.Int32
	tcp := httptest.NewServer(echoMux(&got))
	t.Cleanup(tcp.Close)
	t.Setenv("VAULT_ADDR", tcp.URL)
	if err := checkVaultServer(); !errors.Is(err, errNoVaultSocket) || !strings.Contains(err.Error(), "'pvault lock', then 'pvault unlock'") {
		t.Fatalf("err = %v", err)
	}
}

func TestCredentialRequest_RefusesLooseOrLinkedDir(t *testing.T) {
	var got atomic.Int32
	serveOnSocket(t, serveHandler(t, echoMux(&got)))
	run := filepath.Dir(socketPath())

	if err := os.Chmod(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkVaultServer(); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("0755 run dir: err = %v", err)
	}
	os.Chmod(run, 0o700)

	// The real dir moved aside and a symlink to it in its place.
	moved := run + ".real"
	if err := os.Rename(run, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, run); err != nil {
		t.Fatal(err)
	}
	if err := checkVaultServer(); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("symlinked run dir: err = %v", err)
	}
	if got.Load() != 0 {
		t.Fatal("a password was sent")
	}
}

func TestListenSocket(t *testing.T) {
	shortVaultDir(t)
	path := socketPath()
	// A run dir we own with loose permissions is tightened.
	if err := os.Mkdir(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := listenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Dir(path)); info.Mode().Perm() != 0o700 {
		t.Fatalf("run dir mode %#o, want 0700", info.Mode().Perm())
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %#o, want 0600", info.Mode().Perm())
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	// A live server's socket is never taken over.
	if _, err := listenSocket(path); err == nil || !strings.Contains(err.Error(), "already serving") {
		t.Fatalf("second listener: err = %v", err)
	}

	// A crashed server's leftover socket is replaced.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	ln2, err := listenSocket(path)
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	ln2.Close()

	if _, err := listenSocket("/" + strings.Repeat("x", maxSocketPath)); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long path: err = %v", err)
	}
}

// buildPvault builds pvault into a new directory, signed like 'make build'
// (hardened) or left as the Go linker signs it. Extra build flags make a
// different build of the same code.
func buildPvault(t *testing.T, hardened bool, flags ...string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pvault")
	args := append(append([]string{"build", "-o", bin}, flags...), ".")
	if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if hardened && runtime.GOOS == "darwin" {
		if out, err := exec.Command("codesign", "-s", "-", "-f", "-o", "runtime", bin).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %v\n%s", err, out)
		}
	}
	return bin
}

// startServe runs 'pvault serve --locked' for a new vault in VAULT_DIR on a
// free port and waits for its socket.
func startServe(t *testing.T, pvault string) {
	t.Helper()
	dir := os.Getenv("VAULT_DIR")
	if _, err := vault.Init(dir, "test-password"); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cmd := exec.Command(pvault, "serve", "--locked")
	cmd.Env = append(os.Environ(), "VAULT_DIR="+dir, "VAULT_PORT="+strconv.Itoa(port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", socketPath()); err == nil {
			c.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("pvault serve did not open its socket")
}

// A genuine pvault build, but not the one this command runs, doesn't get
// the password either: the server must be this exact binary.
func TestCredentialRequest_RefusesOtherBinary(t *testing.T) {
	shortVaultDir(t)
	startServe(t, buildPvault(t, true))
	resp, err := credentialRequest("POST", "/vault/unlock", map[string]string{"password": "test-password"})
	if err == nil {
		resp.Body.Close()
		t.Fatal("credentials were sent to another binary")
	}
	if !strings.Contains(err.Error(), "not this pvault") {
		t.Fatalf("err = %v", err)
	}
}

// lock signals a PID from the PID file only if it runs this pvault binary.
func TestIsVaultServerProcess(t *testing.T) {
	if !isVaultServerProcess(os.Getpid()) {
		t.Fatal("this binary should count as the vault server")
	}
	if err := checkSameBuild(os.Getpid()); err != nil {
		t.Fatalf("same process, different build? %v", err)
	}
	other := exec.Command("sleep", "5")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Process.Kill(); other.Wait() })
	if isVaultServerProcess(other.Process.Pid) {
		t.Fatal("another program must not count as the vault server")
	}
}

// pvault ui opens the password page only on this vault's own server, not
// on another program answering "unlocked" on its port.
func TestUIRefusesUnverifiedServer(t *testing.T) {
	impostor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"locked":false}`))
	}))
	defer impostor.Close()
	t.Setenv("VAULT_ADDR", impostor.URL)
	shortVaultDir(t) // no socket: this vault's server isn't running
	if url, err := uiURL(); err == nil {
		t.Fatalf("opened %s on an unverified server", url)
	}
}

// A server verified over the socket but listening elsewhere than VAULT_ADDR:
// whatever answers on VAULT_ADDR must not get tokens or the UI's password.
func TestCheckVaultServer_RefusesOtherAddress(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vault/status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"locked":false,"addr":"127.0.0.1:7300"}`))
	})
	serveOnSocket(t, serveHandler(t, mux))
	squatter := httptest.NewServer(mux)
	t.Cleanup(squatter.Close)
	t.Setenv("VAULT_ADDR", squatter.URL)
	if err := checkVaultServer(); err == nil || !strings.Contains(err.Error(), "127.0.0.1:7300") {
		t.Fatalf("err = %v, want a VAULT_ADDR mismatch", err)
	}
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:7300")
	if err := checkVaultServer(); err != nil {
		t.Fatalf("matching VAULT_ADDR: %v", err)
	}
	// Without VAULT_ADDR the address comes from the vault's port file, and
	// the verified server must listen there too.
	t.Setenv("VAULT_ADDR", "")
	if err := writePort(vaultDir(), 7300); err != nil {
		t.Fatal(err)
	}
	if err := checkVaultServer(); err != nil {
		t.Fatalf("port file matching the server: %v", err)
	}
	if err := writePort(vaultDir(), 7301); err != nil {
		t.Fatal(err)
	}
	if err := checkVaultServer(); err == nil {
		t.Fatal("a port file naming another port was accepted")
	}
}
