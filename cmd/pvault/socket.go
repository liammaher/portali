package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// The profile password and secret key only travel over a private channel to
// the vault server, never over TCP: anything can listen on 127.0.0.1:7200
// while the real server is down. The channel is a Unix socket in the vault
// dir (transport_unix.go), or on Windows a named pipe only this user can open
// (transport_windows.go). Before anything is sent the client checks that the
// server is this user's and runs this pvault binary.

var errNoVaultSocket = errors.New("this vault's server isn't running")

// errUnidentifiedServer is why a server that can't be identified is refused.
func errUnidentifiedServer(err error) error {
	return fmt.Errorf("cannot identify the vault server (%v) — run '%s', then '%s'", err, pvCmd("lock"), pvCmd("unlock"))
}

// checkServerProcess checks that the server process pid, of this user, runs
// the same pvault binary, and build, as this one.
func checkServerProcess(pid int, exe string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !samePath(exe, self) {
		return fmt.Errorf("the vault server is %s, not this pvault (%s) — stop it with '%s %slock', then run '%s'", exe, self, exe, vaultFlag(), pvCmd("unlock"))
	}
	if err := checkSameBuild(pid); err != nil {
		return fmt.Errorf("%w (pvault was rebuilt or reinstalled since it started) — run '%s', then '%s'", err, pvCmd("lock"), pvCmd("unlock"))
	}
	return nil
}

// samePath compares two executables' paths, links resolved; Windows paths
// ignore case.
func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ra, rb)
	}
	return ra == rb
}

// socketClient sends requests only to a verified vault server, one
// connection per request so each one is checked.
var socketClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext:       func(ctx context.Context, _, _ string) (net.Conn, error) { return dialVaultServer(ctx) },
		DisableKeepAlives: true,
	},
}

// approvalClient is socketClient for a request the user approves in person
// (Windows Hello), which waits while they answer.
var approvalClient = &http.Client{Timeout: 4 * time.Minute, Transport: socketClient.Transport}

// credentialRequest sends a request carrying the profile password to the
// verified vault server. Its Host is loopback, as the server requires.
func credentialRequest(method, path string, body any) (*http.Response, error) {
	return privateRequest(socketClient, method, path, body)
}

// approvalRequest is credentialRequest for a request the user approves in
// person.
func approvalRequest(method, path string, body any) (*http.Response, error) {
	return privateRequest(approvalClient, method, path, body)
}

func privateRequest(client *http.Client, method, path string, body any) (*http.Response, error) {
	resp, err := doRequestWith(client, "http://localhost", method, path, body, "")
	// Drop net/http's `Post "http://localhost/...":` prefix.
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return resp, err
}

// checkVaultServer verifies the vault server before the user is asked for a
// password or fingerprint that would go to it.
func checkVaultServer() error {
	resp, err := credentialRequest("GET", "/vault/status", nil)
	if err != nil {
		// A server from before the socket, or a program squatting the port.
		if errors.Is(err, errNoVaultSocket) && portHasVault() {
			return fmt.Errorf("%w: the server on %s is an older pvault or another program — run '%s', then '%s'", errNoVaultSocket, serverAddr(), pvCmd("lock"), pvCmd("unlock"))
		}
		return err
	}
	defer resp.Body.Close()
	// Tokens and the UI's password go to VAULT_ADDR over TCP, so it must be
	// the server just verified, not something else on another port or on
	// localhost's other address.
	var status struct {
		Addr     string `json:"addr"`
		Kind     string `json:"kind"`
		Approver string `json:"approver"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status); err != nil {
		return fmt.Errorf("vault server status: %w", err)
	}
	if u, err := url.Parse(serverAddr()); err != nil || u.Host != status.Addr {
		if os.Getenv("VAULT_ADDR") == "" {
			return fmt.Errorf("this vault's server listens on %s, but pvault expects %s (from %s, or 7200 without one; a server started with VAULT_PORT needs VAULT_ADDR too) — run '%s', then '%s'", status.Addr, serverAddr(), portPath(vaultDir()), pvCmd("lock"), pvCmd("unlock"))
		}
		return fmt.Errorf("VAULT_ADDR %s isn't this vault's server, which listens on %s — set VAULT_ADDR=http://%s or unset it", serverAddr(), status.Addr, status.Addr)
	}
	verifiedKind = status.Kind
	verifiedApprover = status.Approver
	if verifiedKind == "" {
		verifiedKind = vault.KindPersona // a server from before vault kinds
	}
	return nil
}

// isVaultServerProcess reports whether pid runs this pvault binary, by the
// kernel's path of its executable (which survives a reinstall with rm + cp),
// so a planted PID file can't get another process signalled.
func isVaultServerProcess(pid int) bool {
	exe, err := processExe(pid)
	if err != nil {
		return false
	}
	self, err := os.Executable()
	return err == nil && samePath(exe, self)
}
