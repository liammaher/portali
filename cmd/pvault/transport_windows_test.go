package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// listenPipe starts a server on this vault's pipe, as pvault serve does: the
// server is this test binary, so the CLI's checks accept it.
func listenPipe(t *testing.T) net.Listener {
	t.Helper()
	ln, err := listenSocket(socketPath())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vault/status", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	mux.HandleFunc("POST /vault/stop", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln
}

func TestPipe_DialVerifiesThisServer(t *testing.T) {
	shortVaultDir(t)
	listenPipe(t)
	conn, err := dialVaultServer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if pid, ok := untrustedSocketServer(); ok {
		t.Fatalf("this binary's own server counted as untrusted (pid %d)", pid)
	}
}

// The file naming the pipe is held open by the server: a second server sees
// it can't be removed and refuses, and closing the server removes it.
func TestPipe_OneServerPerVault(t *testing.T) {
	shortVaultDir(t)
	ln := listenPipe(t)
	if _, err := listenSocket(socketPath()); err == nil || !strings.Contains(err.Error(), "already serving") {
		t.Fatalf("second server: %v", err)
	}
	if err := os.Remove(socketPath()); err == nil {
		t.Fatal("a live server's file could be removed")
	}
	ln.Close()
	if _, err := os.Stat(socketPath()); err == nil {
		t.Fatal("the file naming the pipe outlived the server")
	}
}

// A file left by a server that crashed names a pipe nobody may own any more:
// the next server replaces it, and the CLI never takes it for a server.
func TestPipe_StaleFile(t *testing.T) {
	shortVaultDir(t)
	os.Mkdir(filepath.Dir(socketPath()), 0o700)
	stale := `\\.\pipe\pvault-` + strings.Repeat("0", 32)
	if err := os.WriteFile(socketPath(), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dialVaultServer(context.Background()); err != errNoVaultSocket {
		t.Fatalf("stale file: %v", err)
	}
	os.WriteFile(socketPath(), []byte(`\\.\pipe\someone-else`), 0o600)
	if _, err := dialVaultServer(context.Background()); err == nil || !strings.Contains(err.Error(), "doesn't name") {
		t.Fatalf("odd name: %v", err)
	}
	listenPipe(t)
	if name, err := serverPipe(); err != nil || name == stale {
		t.Fatalf("pipe %q, %v", name, err)
	}
}

// lock stops a server over the pipe, and only the one it means.
func TestAskServerToStop(t *testing.T) {
	shortVaultDir(t)
	listenPipe(t)
	if askServerToStop(os.Getpid() + 1) {
		t.Fatal("stopped a server that isn't the pid asked for")
	}
	done := make(chan bool)
	go func() { done <- askServerToStop(os.Getpid()) }()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("not stopped")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}
}
