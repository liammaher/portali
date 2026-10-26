package main

import (
	"fmt"
	"os"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdInit() {
	positionalArgs(os.Args[2:], "usage: pvault init", 0, 0)
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

	sk, err := vault.Init(dir, pw)
	if err != nil {
		fatal("%v", err)
	}

	fmt.Println("Vault initialized successfully.")
	fmt.Println()
	fmt.Println("Your secret key (save this somewhere safe):")
	fmt.Printf("  %s\n", sk)
	fmt.Println()
	fmt.Printf("Secret key also saved to: %s\n", secretKeyPath())
	fmt.Printf("Vault database: %s/vault.db\n", dir)
	fmt.Println()
	fmt.Println("Next: run 'pvault unlock' to start using your vault.")
}
