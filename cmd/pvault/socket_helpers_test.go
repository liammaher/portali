package main

import (
	"net"
	"net/http"
	"os"
	"testing"
)

// shortVaultDir sets VAULT_DIR to a new short directory: t.TempDir paths
// are too long for a Unix socket on macOS (104 bytes).
func shortVaultDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "pv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("VAULT_DIR", dir)
	return dir
}

// serveOnSocket starts serve on the vault socket of a new VAULT_DIR, as
// pvault serve does. The server is this test binary, which isn't hardened.
func serveOnSocket(t *testing.T, serve func(net.Listener)) {
	t.Helper()
	shortVaultDir(t)
	saved := requireHardenedRuntime
	requireHardenedRuntime = false
	t.Cleanup(func() { requireHardenedRuntime = saved })
	ln, err := listenSocket(socketPath())
	if err != nil {
		t.Fatal(err)
	}
	if ln != nil { // nil where there is no socket
		serve(ln)
	}
}

// serveHandler is a serveOnSocket function for a plain handler.
func serveHandler(t *testing.T, h http.Handler) func(net.Listener) {
	return func(ln net.Listener) {
		srv := &http.Server{Handler: h}
		go srv.Serve(ln)
		t.Cleanup(func() { srv.Close() })
	}
}
