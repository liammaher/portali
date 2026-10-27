//go:build !linux && !darwin && !windows

package main

import (
	"context"
	"errors"
	"net"
)

// Without a way to tell who listens on a private channel here, there is no
// channel: commands that need the password refuse rather than send it over
// TCP. See socket.go for the protected platforms.

var errNoPrivateChannel = errors.New("pvault can't keep your password safe on this system (macOS, Linux and Windows only)")

func socketPath() string { return "" }

// listenSocket refuses, so pvault serve (and unlock) doesn't start a server
// here that lock couldn't stop without a password.
func listenSocket(string) (net.Listener, error) { return nil, errNoPrivateChannel }

func dialVaultServer(context.Context) (net.Conn, error) { return nil, errNoPrivateChannel }

func untrustedSocketServer() (int, bool) { return 0, false }

func processExe(int) (string, error) { return "", errNoPrivateChannel }

func checkSameBuild(int) error { return nil }
