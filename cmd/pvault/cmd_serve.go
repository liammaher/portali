package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lovincyrus/personal-vault/internal/api"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

var passwordFromStdin, startLocked bool

func init() {
	for _, arg := range os.Args {
		switch arg {
		case "--password-stdin":
			passwordFromStdin = true
		case "--locked":
			startLocked = true
		}
	}
}

func cmdServe() {
	dir := vaultDir()
	v, err := vault.Open(dir)
	if err != nil {
		fatal("open vault: %v", err)
	}
	defer v.Close()

	// Get credentials from stdin pipe (sent by unlock command) or prompt.
	// A server started --locked gets none: pvault-touchid unlocks it over the API.
	var pw, sk string
	switch {
	case startLocked:
	case passwordFromStdin:
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			pw = strings.TrimSpace(scanner.Text())
		}
		if scanner.Scan() {
			sk = strings.TrimSpace(scanner.Text())
		}
		if pw == "" || sk == "" {
			fatal("failed to read credentials from stdin")
		}
	default:
		pw, err = promptPassword("Profile password: ")
		if err != nil {
			fatal("reading password: %v", err)
		}
		sk, err = readSecretKey()
		if err != nil {
			fatal("%v", err)
		}
	}

	// The unlock token stays in this process only. CLI commands get their
	// own password-gated grants, so nothing reusable is written to disk.
	if !startLocked {
		if _, err := v.Unlock(pw, sk); err != nil {
			fatal("unlock: %v", err)
		}
	}
	removeLegacySessionFile()

	// Note: Go strings are immutable; setting pw="" does not zero heap memory.
	// Accept this limitation — use []byte for passwords if zeroing is critical.

	addr := "127.0.0.1:7200"
	if a := os.Getenv("VAULT_PORT"); a != "" {
		addr = "127.0.0.1:" + a
	}

	// The socket comes first: once the TCP port answers, the socket that
	// credentials go to (socket.go) is already there.
	sock, err := listenSocket(socketPath())
	if err != nil {
		fatal("start server: %v", err)
	}

	srv := api.New(v, addr)
	ln, err := srv.Start()
	if err != nil {
		if sock != nil {
			sock.Close()
		}
		fatal("start server: %v", err)
	}
	if sock != nil {
		srv.Serve(sock)
	}

	fmt.Fprintf(os.Stderr, "Vault server listening on %s\n", ln.Addr())

	// Wait for signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Fprintln(os.Stderr, "\nShutting down...")
	v.Lock()
	srv.Stop(context.Background())
	removeLegacySessionFile()
	removePID()
}
