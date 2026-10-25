package main

import (
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"os/user"
	"runtime"
	"testing"
)

func TestKeychainServicePattern(t *testing.T) {
	for _, ok := range []string{"pvault-mcp", "a", "x.y_z-1"} {
		if !keychainServicePattern.MatchString(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"", "has space", "semi;colon", "quote\"", "new\nline"} {
		if keychainServicePattern.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestStoreInKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Keychain only")
	}
	if testing.Short() {
		t.Skip("touches the login Keychain")
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 8)
	rand.Read(b)
	service := "pvault-test-" + hex.EncodeToString(b)
	t.Cleanup(func() {
		exec.Command("security", "delete-generic-password", "-a", u.Username, "-s", service).Run()
	})

	if err := checkKeychain(service); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"aaaa1111", "bbbb2222"} { // second call replaces the first
		if err := storeInKeychain(service, token); err != nil {
			t.Fatalf("store %s: %v", token, err)
		}
	}
	got, err := exec.Command("security", "find-generic-password", "-a", u.Username, "-s", service, "-w").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "bbbb2222\n" {
		t.Fatalf("got %q", got)
	}
}
