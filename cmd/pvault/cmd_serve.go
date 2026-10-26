package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lovincyrus/personal-vault/internal/api"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdServe() {
	passwordFromStdin, startLocked, err := parseServeArgs(os.Args[2:])
	if err != nil {
		fatal("%v\nusage: pvault serve [--password-stdin | --locked]", err)
	}
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
		if pw, sk, err = readStdinCredentials(os.Stdin); err != nil {
			fatal("%v", err)
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

// readStdinCredentials reads the password and secret key that 'pvault unlock'
// pipes in, one per line. The password is kept as typed, spaces included;
// only the line ending is dropped.
func readStdinCredentials(r io.Reader) (pw, sk string, err error) {
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		pw = strings.TrimRight(scanner.Text(), "\r")
	}
	if scanner.Scan() {
		sk = strings.TrimSpace(scanner.Text())
	}
	if pw == "" || sk == "" {
		return "", "", errors.New("failed to read credentials from stdin")
	}
	return pw, sk, nil
}

// parseServeArgs reads serve's options: --password-stdin (used by 'pvault
// unlock') or --locked (Touch ID unlocks it later). Anything else is an error,
// so a typo never falls back to a password prompt.
func parseServeArgs(args []string) (passwordFromStdin, startLocked bool, err error) {
	for _, arg := range args {
		switch arg {
		case "--password-stdin":
			passwordFromStdin = true
		case "--locked":
			startLocked = true
		default:
			return false, false, fmt.Errorf("unknown option %q", arg)
		}
	}
	if passwordFromStdin && startLocked {
		return false, false, errors.New("--password-stdin and --locked can't be combined")
	}
	return passwordFromStdin, startLocked, nil
}
