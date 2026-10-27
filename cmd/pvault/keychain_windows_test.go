package main

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/wincred"
)

// Credential Manager keys on the name alone: --keychain never overwrites, or
// reads as its own, what was saved there for another account (a PEC password
// under a mistyped name).
func TestKeychain_CredentialOfAnotherAccount(t *testing.T) {
	b := make([]byte, 8)
	rand.Read(b)
	name := "pvault-test-" + hex.EncodeToString(b)
	t.Cleanup(func() { wincred.Delete(name) })
	if err := wincred.Write(name, "someone@pec.it", "app-password"); err != nil {
		t.Fatal(err)
	}
	if err := checkKeychain(name); err == nil || !strings.Contains(err.Error(), "someone@pec.it") {
		t.Fatalf("checkKeychain: %v", err)
	}
	if got, err := readKeychain(name); got != "" || err != nil {
		t.Fatalf("readKeychain: %q, %v", got, err)
	}
	wincred.Delete(name)
	if err := storeInKeychain(name, "abc123"); err != nil {
		t.Fatal(err)
	}
	if got, err := readKeychain(name); got != "abc123" || err != nil {
		t.Fatalf("own token: %q, %v", got, err)
	}
}
