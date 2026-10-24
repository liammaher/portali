package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

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
		pw, err := promptPassword("Profile password: ")
		if err != nil {
			fatal("reading password: %v", err)
		}
		sk, err := readSecretKey()
		if err != nil {
			fatal("%v", err)
		}
		reUnlock(pw, sk)
		return
	}

	// Nothing on the port — clean up any stale PID file
	removePID()

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
	resp, err := publicRequest("POST", "/vault/unlock", body)
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
