package main

import (
	"encoding/json"
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
	// Probe the port first — catches stale servers even if the PID file is gone.
	if portHasVault() {
		if isVaultUnlocked() {
			fmt.Println("Vault is already unlocked (server running).")
			return
		}
		// Server running but vault auto-locked — re-unlock via API
		unlockRunningServer(false)
		return
	}

	// Nothing on the port — clean up any stale PID file
	removePID()

	// With Touch ID, start the server locked and let the helper unlock it;
	// the typed password stays the fallback.
	if touchIDEnrolled() {
		verified, err := startBackgroundServer(vaultDir(), "", "")
		if err != nil {
			fatal("%v", err)
		}
		if !verified {
			fatal("vault server is still starting — run 'pvault unlock' again in a moment")
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

	verified, err := startBackgroundServer(vaultDir(), pw, sk)
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
				fatal("vault still locked after Touch ID unlock")
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
	reUnlock(pw, sk)
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
	resp, err := publicRequest("GET", "/vault/status", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var status struct {
		Locked bool `json:"locked"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return false
	}
	return !status.Locked
}

func reUnlock(password, secretKey string) {
	body := map[string]string{
		"password":   password,
		"secret_key": secretKey,
	}
	resp, err := credentialRequest("POST", "/vault/unlock", body)
	if err != nil {
		fatal("re-unlock request: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode >= 400 {
		fatal("unlock failed: %s", result.Error)
	}
	// The unlock token is deliberately discarded: CLI commands authenticate
	// with a fresh password-gated grant instead of a stored token.
	fmt.Println("Vault unlocked. Server running on", serverAddr())
}
