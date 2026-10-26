package main

import (
	"fmt"
	"io"
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
	if err := finishInit(os.Stdout, dir, sk, opts.Kind); err != nil {
		fatal("vault created, but %v", err)
	}
}

// finishInit reports a new vault and gives it its port. The secret key is
// printed first: the vault exists either way, and an error about ports must
// not hide it.
func finishInit(w io.Writer, dir, sk, kind string) error {
	if kind == vault.KindImpresa {
		fmt.Fprintln(w, "Business vault initialized successfully.")
	} else {
		fmt.Fprintln(w, "Vault initialized successfully.")
	}
	fmt.Fprintln(w)
	printSecretKey(w, sk)
	fmt.Fprintf(w, "Secret key also saved to: %s\n", secretKeyPath())
	port, err := ensurePort(dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Vault database: %s/vault.db\n", dir)
	fmt.Fprintf(w, "Server address: http://127.0.0.1:%d\n", port)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Next: run '%s' to start using your vault.\n", pvCmd("unlock"))
	return nil
}

// printSecretKey shows a new vault's secret key.
func printSecretKey(w io.Writer, sk string) {
	fmt.Fprintln(w, "Your secret key (save this somewhere safe):")
	fmt.Fprintf(w, "  %s\n", sk)
	fmt.Fprintln(w)
}
