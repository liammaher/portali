package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdUnlock() {
	positionalArgs(os.Args[2:], "usage: pvault unlock", 0, 0)
	// Probe the port first — catches stale servers even if the PID file is gone.
	if portHasVault() {
		if isVaultUnlocked() {
			// Anything can answer "unlocked" on the port; only say so for
			// this vault's own server.
			if err := checkVaultServer(); err != nil {
				fatal("%v", err)
			}
			fmt.Println("Vault is already unlocked (server running).")
			return
		}
		// Server running but vault auto-locked — re-unlock via API
		unlockRunningServer(false)
		return
	}

	// Nothing on the port — clean up any stale PID file
	removeStalePID()

	// With Touch ID, start the server locked and let the helper unlock it;
	// the typed password stays the fallback.
	if touchIDEnrolled() {
		verified, err := startBackgroundServer("", "")
		if err != nil {
			fatal("%v", err)
		}
		if !verified {
			stopOwnServer()
			fatal("vault server didn't start in time — run 'pvault unlock' again")
		}
		unlockRunningServer(true)
		return
	}

	pw, err := promptPassword("Profile password: ")
	if err != nil {
		fatal("reading password: %v", err)
	}

	sk, err := readSecretKey()
	if err != nil {
		fatal("%v", err)
	}

	// Check the password here so a typo gets a clear error instead of a
	// server that silently fails to start.
	if err := checkCredentials(pw, sk); err != nil {
		fatal("%v", err)
	}

	verified, err := startBackgroundServer(pw, sk)
	if err != nil {
		fatal("%v", err)
	}
	if !verified {
		fmt.Println("Vault server started (could not verify status).")
		return
	}
	fmt.Println("Vault unlocked. Server running on", serverAddr())
}

// unlockRunningServer unlocks a running, locked server: with Touch ID when
// it's on, else (or if that fails in a terminal) with the typed password.
// When this command just started the server and no one at a terminal can
// unlock it, the server is stopped again rather than left running locked.
func unlockRunningServer(startedHere bool) {
	giveUp := func(msg string, args ...any) {
		if startedHere {
			stopOwnServer()
		}
		fatal(msg, args...)
	}
	// Whatever answered on the TCP port, the password only goes to this
	// vault's own server; check it before asking for anything.
	if err := checkVaultServer(); err != nil {
		giveUp("%v", err)
	}
	if touchIDEnrolled() {
		_, err := runTouchID("", "unlock")
		if err == nil {
			if !isVaultUnlocked() {
				giveUp("vault still locked after Touch ID unlock")
			}
			fmt.Println("Vault unlocked with Touch ID. Server running on", serverAddr())
			return
		}
		if !term.IsTerminal(int(syscall.Stdin)) {
			giveUp("%v", err)
		}
		fmt.Fprintf(os.Stderr, "%v — enter your password instead.\n", err)
	}
	pw, err := promptPassword("Profile password: ")
	if err != nil {
		giveUp("reading password: %v", err)
	}
	sk, err := readSecretKey()
	if err != nil {
		giveUp("%v", err)
	}
	if err := reUnlock(pw, sk); err != nil {
		giveUp("%v", err)
	}
	fmt.Println("Vault unlocked. Server running on", serverAddr())
}

// checkCredentials verifies the password and secret key against the vault
// database without starting a server.
func checkCredentials(pw, sk string) error {
	// vault.Open creates a missing database, which would make a later
	// 'pvault init' refuse; check first.
	if _, err := os.Stat(filepath.Join(vaultDir(), "vault.db")); err != nil {
		return fmt.Errorf("no vault found in %s — run 'pvault init' first", vaultDir())
	}
	v, err := vault.Open(vaultDir())
	if err != nil {
		return fmt.Errorf("open vault: %w", err)
	}
	defer v.Close()
	return v.CheckCredentials(pw, sk)
}

// portHasVault probes the server address with GET /vault/status.
// Returns true if a vault server responds, false otherwise.
func portHasVault() bool {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(serverAddr() + "/vault/status")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func isVaultUnlocked() bool {
	status, err := serverStatus()
	return err == nil && !status.Locked
}

func reUnlock(password, secretKey string) error {
	body := map[string]string{
		"password":   password,
		"secret_key": secretKey,
	}
	resp, err := credentialRequest("POST", "/vault/unlock", body)
	if err != nil {
		return fmt.Errorf("re-unlock request: %w", err)
	}
	// The unlock token is deliberately discarded: CLI commands authenticate
	// with a fresh password-gated grant instead of a stored token.
	if err := apiResult(resp, nil); err != nil {
		return fmt.Errorf("unlock failed: %w", err)
	}
	return nil
}
