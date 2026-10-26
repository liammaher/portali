package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildDebugHelper compiles cmd/pvault-touchid with -D DEBUG, which adds
// the describe-grant command that prints a grant's prompt without touching
// the Keychain or a server.
func buildDebugHelper(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the Touch ID helper is macOS only")
	}
	swiftc, err := exec.LookPath("swiftc")
	if err != nil {
		t.Skip("swiftc not installed")
	}
	bin := filepath.Join(t.TempDir(), "pvault-touchid-debug")
	out, err := exec.Command(swiftc, "-D", "DEBUG", "-swift-version", "5",
		filepath.Join("..", "pvault-touchid", "main.swift"), "-o", bin).CombinedOutput()
	if err != nil {
		t.Fatalf("swiftc: %v\n%s", err, out)
	}
	return bin
}

func TestTouchIDHelper_PromptWording(t *testing.T) {
	helper := buildDebugHelper(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--scope", "identity.full_name,residenza.cap"},
			"read identity.full_name, residenza.cap from your vault for 2 minutes"},
		// Append and write grants can read too, and a category covers its documents.
		{[]string{"--scope", "spid.*", "--access", "append"},
			"read and add new entries to all your spid fields and documents in your vault for 2 minutes"},
		{[]string{"--scope", "tari.documents", "--access", "append"},
			"read and add new entries to your tari documents in your vault for 2 minutes"},
		{[]string{"--scope", "passaporto.*", "--access", "write"},
			"read, change and delete all your passaporto fields and documents in your vault for 2 minutes"},
		// "*" is named alone, never at the end of a list.
		{[]string{"--scope", "identity.full_name,spid.*,*", "--access", "write"},
			"read, change and delete everything in your vault, your family's data included for 2 minutes"},
		// Family members: one member's data, or everyone's.
		{[]string{"--scope", "leo:*"}, "read everything about leo from your vault for 2 minutes"},
		{[]string{"--scope", "leo:cie.*", "--access", "append"},
			"read and add new entries to all of leo's cie fields and documents in your vault for 2 minutes"},
		{[]string{"--scope", "*:identity.*"}, "read everyone's identity fields and documents from your vault for 2 minutes"},
		{[]string{"--scope", "leo:cie.numero,giulia:cie.documents"},
			"read leo:cie.numero, giulia's cie documents from your vault for 2 minutes"},
		// Durations round up.
		{[]string{"--scope", "residenza.cap", "--ttl", "89"}, "read residenza.cap from your vault for 2 minutes"},
		{[]string{"--scope", "residenza.cap", "--ttl", "45"}, "read residenza.cap from your vault for 45 seconds"},
		{nil, "get full access to your vault, including settings and agent tokens, for 2 minutes"},
	}
	for _, c := range cases {
		out, err := exec.Command(helper, append([]string{"describe-grant"}, c.args...)...).CombinedOutput()
		if err != nil {
			t.Errorf("%q: %v\n%s", c.args, err, out)
			continue
		}
		if got := strings.TrimSpace(string(out)); got != c.want {
			t.Errorf("%q:\n got  %q\n want %q", c.args, got, c.want)
		}
	}
}

// With several vaults the prompt names the one a fingerprint is for.
func TestTouchIDHelper_PromptNamesVault(t *testing.T) {
	helper := buildDebugHelper(t)
	cases := []struct {
		dir  string
		args []string
		want string
	}{
		{".pvault-studio", []string{"--scope", "business.*"}, "read all your business fields and documents from your studio vault for 2 minutes"},
		{".pvault-studio", []string{"--scope", "*"}, "read everything in your studio vault, everyone's data in it included for 2 minutes"},
		{".pvault-studio", nil, "get full access to your studio vault, including settings and agent tokens, for 2 minutes"},
		{".pvault", []string{"--scope", "*"}, "read everything in your vault, your family's data included for 2 minutes"},
		{"elsewhere", []string{"--scope", "cie.*"}, "read all your cie fields and documents from your vault for 2 minutes"},
	}
	for _, c := range cases {
		cmd := exec.Command(helper, append([]string{"describe-grant"}, c.args...)...)
		cmd.Env = append(os.Environ(), "VAULT_DIR="+filepath.Join(t.TempDir(), c.dir))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("%s %q: %v\n%s", c.dir, c.args, err, out)
			continue
		}
		if got := strings.TrimSpace(string(out)); got != c.want {
			t.Errorf("%s %q:\n got  %q\n want %q", c.dir, c.args, got, c.want)
		}
	}
}

func TestTouchIDHelper_RejectsOddScopes(t *testing.T) {
	helper := buildDebugHelper(t)
	for _, scope := range []string{
		"identity.full_name\n",
		"identity.full_name\r\n,spid.*",
		"identity.full_name\u2028",
		"identity." + strings.Repeat("x", 100),
		"x",
		"",
		strings.Repeat("a.b,", 10) + "c.d",
		// Malformed member prefixes, and io (the owner has no prefix).
		"leo:", ":cie.*", "leo:cie:x.y", "Leo:*", "leo:*\n", "*:*", "io:*", "io:cie.*",
		"leo:cie", "leo_x:cie.*", strings.Repeat("a", 33) + ":*",
	} {
		out, err := exec.Command(helper, "describe-grant", "--scope", scope).CombinedOutput()
		if err == nil {
			t.Errorf("scope %q accepted: %s", scope, out)
		}
	}
}

// Unknown, repeated or incomplete options are refused, never guessed at.
func TestTouchIDHelper_RejectsBadGrantArgs(t *testing.T) {
	helper := buildDebugHelper(t)
	for _, args := range [][]string{
		{"--scope", "identity.*", "--scope", "*"},
		{"--scope"},
		{"--access", "read"},
		{"--scope", "identity.*", "--force", "x"},
	} {
		out, err := exec.Command(helper, append([]string{"describe-grant"}, args...)...).CombinedOutput()
		if err == nil {
			t.Errorf("%q accepted: %s", args, out)
		}
	}
}
