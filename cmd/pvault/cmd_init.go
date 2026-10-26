package main

import (
	"fmt"
	"os"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

const initUsage = `usage: pvault [--vault <name>] init [--impresa]
  --impresa creates a business vault: its own data is the business's, and its
  people (titolare, soci, employees, clients) each have a ruolo.
  --vault <name> creates it in ~/.pvault-<name>, with its own server port.`

func cmdInit() {
	opts := vault.InitOptions{}
	for _, arg := range os.Args[2:] {
		if arg != "--impresa" || opts.Kind != "" {
			fatal(initUsage)
		}
		opts.Kind = vault.KindImpresa
	}
	dir := vaultDir()
	if err := vault.CanInit(dir); err != nil {
		fatal("%v", err)
	}

	pw, err := promptPassword("Profile password: ")
	if err != nil {
		fatal("reading password: %v", err)
	}
	if len(pw) < 8 {
		fatal("password must be at least 8 characters")
	}

	confirm, err := promptPassword("Confirm password: ")
	if err != nil {
		fatal("reading confirmation: %v", err)
	}
	if pw != confirm {
		fatal("passwords do not match")
	}

	sk, err := vault.InitWithOptions(dir, pw, opts)
	if err != nil {
		fatal("%v", err)
	}
	port, err := ensurePort(dir)
	if err != nil {
		fatal("vault created, but %v", err)
	}

	if opts.Kind == vault.KindImpresa {
		fmt.Println("Business vault initialized successfully.")
	} else {
		fmt.Println("Vault initialized successfully.")
	}
	fmt.Println()
	fmt.Println("Your secret key (save this somewhere safe):")
	fmt.Printf("  %s\n", sk)
	fmt.Println()
	fmt.Printf("Secret key also saved to: %s\n", secretKeyPath())
	fmt.Printf("Vault database: %s/vault.db\n", dir)
	fmt.Printf("Server address: http://127.0.0.1:%d\n", port)
	fmt.Println()
	fmt.Printf("Next: run 'pvault %sunlock' to start using your vault.\n", vaultFlag())
}
