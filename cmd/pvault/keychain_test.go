package main

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"testing"
)

func TestKeychainNamePattern(t *testing.T) {
	for _, ok := range []string{"pvault-mcp", "a", "x.y_z-1", "_svc"} {
		if !keychainNamePattern.MatchString(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	// "--ttl" is what --keychain swallows when its value is forgotten.
	for _, bad := range []string{"", "has space", "semi;colon", "quote\"", "new\nline", "-U", "--ttl"} {
		if keychainNamePattern.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestTokenRefIsServerHash(t *testing.T) {
	// The server stores hex(sha256(token)) and accepts it as a revoke reference.
	got := tokenRef("abc")
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("tokenRef = %s, want %s", got, want)
	}
}

// TestStoreInKeychain writes to the real login Keychain, so it only runs when
// asked: PVAULT_KEYCHAIN_TEST=1 go test ./cmd/pvault/
func TestStoreInKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Keychain only")
	}
	if os.Getenv("PVAULT_KEYCHAIN_TEST") != "1" {
		t.Skip("touches the login Keychain; set PVAULT_KEYCHAIN_TEST=1 to run")
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 8)
	rand.Read(b)
	service := "pvault-test-" + hex.EncodeToString(b)
	t.Cleanup(func() {
		exec.Command(securityPath, "delete-generic-password", "-a", u.Username, "-s", service).Run()
	})

	if err := checkKeychain(service); err != nil {
		t.Fatal(err)
	}
	if got, err := readKeychain(service); err != nil || got != "" {
		t.Fatalf("missing item: got %q, %v; want empty, nil", got, err)
	}
	for _, token := range []string{"aaaa1111", "bbbb2222"} { // second call replaces the first
		if err := storeInKeychain(service, token); err != nil {
			t.Fatalf("store %s: %v", token, err)
		}
	}
	if got, err := readKeychain(service); err != nil || got != "bbbb2222" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// A token with a line break would add a second `security -i` command; it is
// refused before security runs.
func TestStoreInKeychain_RefusesNonHexToken(t *testing.T) {
	for _, bad := range []string{"", "abc\ndelete-keychain login.keychain", "abc def", "ABC", "x"} {
		if err := storeInKeychain("pvault-test-never-used", bad); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Errorf("storeInKeychain(%q) = %v, want malformed token", bad, err)
		}
	}
}
