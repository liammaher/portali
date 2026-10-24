package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

var onboardFields = []struct {
	prompt  string
	fieldID string
}{
	{"Nome", "identity.first_name"},
	{"Cognome", "identity.last_name"},
	{"Codice fiscale", "identity.codice_fiscale"},
	{"Email", "identity.email"},
	{"Cellulare (+39 ...)", "identity.phone"},
	{"Residenza — via / piazza", "residenza.indirizzo"},
	{"Civico", "residenza.civico"},
	{"CAP", "residenza.cap"},
	{"Comune", "residenza.comune"},
	{"Provincia (sigla, e.g. NA)", "residenza.provincia"},
}

func cmdOnboard() {
	dir := vaultDir()

	// Check if vault already exists
	if _, err := os.Stat(dir + "/vault.db"); err == nil {
		fatal("vault already initialized at %s — use 'pvault unlock' instead", dir)
	}

	fmt.Println("Create your vault")

	pw, err := promptPassword("  Profile password: ")
	if err != nil {
		fatal("reading password: %v", err)
	}
	if len(pw) < 8 {
		fatal("password must be at least 8 characters")
	}

	confirm, err := promptPassword("  Confirm: ")
	if err != nil {
		fatal("reading confirmation: %v", err)
	}
	if pw != confirm {
		fatal("passwords do not match")
	}

	sk, err := vault.Init(dir, pw)
	if err != nil {
		fatal("%v", err)
	}

	fmt.Println()
	fmt.Println("Your secret key (save this somewhere safe):")
	fmt.Printf("  %s\n", sk)
	fmt.Println()

	verified, err := startBackgroundServer(dir, pw, sk)
	if err != nil {
		fatal("%v", err)
	}
	if !verified {
		fmt.Println("Vault server started (could not verify status).")
		return
	}

	fmt.Printf("Vault unlocked. Server running on %s\n", serverAddr())
	fmt.Println()

	// Reuse the credentials just entered for a grant covering the prompts below.
	token, err := requestGrant(pw, sk, "10m")
	if err != nil {
		fatal("authenticating: %v", err)
	}
	cliToken = token

	// Prompt for common fields
	fmt.Println("Let's add some basics (press Enter to skip any):")
	reader := bufio.NewReader(os.Stdin)
	saved := 0

	for _, f := range onboardFields {
		fmt.Printf("  %s: ", f.prompt)
		line, _ := reader.ReadString('\n')
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}

		resp, err := apiRequest("PUT", "/vault/fields/"+f.fieldID, map[string]string{
			"value": value,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not save %s: %v\n", f.fieldID, err)
			continue
		}
		if err := apiResult(resp, nil); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not save %s: %v\n", f.fieldID, err)
			continue
		}
		saved++
	}

	fmt.Println()
	if saved > 0 {
		fmt.Printf("Done — %d field(s) saved. Your vault is ready.\n", saved)
	} else {
		fmt.Println("Done — your vault is ready.")
	}
	fmt.Println("Run 'pvault status' to see what's stored.")
}
