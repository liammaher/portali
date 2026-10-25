//go:build !linux && !darwin

package main

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Without a way to identify the peer of a Unix socket here, credentials go
// over TCP as before; see socket.go for the protected platforms.

func socketPath() string { return "" }

func listenSocket(string) (net.Listener, error) { return nil, nil }

func credentialRequest(method, path string, body any) (*http.Response, error) {
	return publicRequest(method, path, body)
}

func checkVaultServer() error { return nil }

// isVaultServerProcess reports whether pid is a running pvault process.
func isVaultServerProcess(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	name := filepath.Base(strings.TrimSpace(string(out)))
	if strings.Contains(name, "pvault") {
		return true
	}
	self, err := os.Executable()
	return err == nil && name == filepath.Base(self)
}
