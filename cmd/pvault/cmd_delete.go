package main

import (
	"fmt"
	"os"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func cmdDelete() {
	if len(os.Args) < 3 {
		fatal("usage: pvault delete <id>")
	}
	id := os.Args[2]
	// A well-formed ID first, so the Touch ID prompt names exactly one field.
	if err := vault.ValidateFieldID(id); err != nil {
		fatal("%v", err)
	}
	needGrant(id, "write")

	resp, err := apiRequest("DELETE", "/vault/fields/"+id, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}

	var result map[string]string
	if err := apiResult(resp, &result); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Deleted %s\n", id)
}
