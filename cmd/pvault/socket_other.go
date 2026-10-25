//go:build !linux && !darwin

package main

import (
	"errors"
	"net"
	"net/http"
)

// Without a way to identify the peer of a Unix socket here, credentials go
// over TCP as before; see socket.go for the protected platforms.

var requireHardenedRuntime = false

func socketPath() string { return "" }

func listenSocket(string) (net.Listener, error) { return nil, nil }

func credentialRequest(method, path string, body any) (*http.Response, error) {
	return publicRequest(method, path, body)
}

func checkVaultServer() error { return nil }

var errNoVaultSocket = errors.New("this vault's server isn't running")
