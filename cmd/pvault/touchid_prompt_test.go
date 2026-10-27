package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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

// loadPromptFixture reads internal/vault/testdata/prompts.json, which also
// holds the vault's own wording (approval_prompt_test.go) for Windows Hello.
func loadPromptFixture(t *testing.T) (f struct {
	Grants []struct {
		Vault  string
		Scope  *string
		Access string
		TTL    int
		Want   string
	}
	Signs []struct {
		Vault, Name, Hash8, Requester, Want string
		Pages                               int
	}
	RefusedScopes  []string
	AcceptedScopes []string
}) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "vault", "testdata", "prompts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// describe runs a debug helper command with VAULT_DIR set to a folder named
// vault (.pvault if empty), so the prompt names that vault.
func describe(t *testing.T, helper, vault string, args ...string) (string, error) {
	t.Helper()
	if vault == "" {
		vault = ".pvault"
	}
	cmd := exec.Command(helper, args...)
	cmd.Env = append(os.Environ(), "VAULT_DIR="+filepath.Join(t.TempDir(), vault))
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestTouchIDHelper_PromptWording(t *testing.T) {
	helper := buildDebugHelper(t)
	for _, c := range loadPromptFixture(t).Grants {
		args := []string{"describe-grant"}
		if c.Scope != nil {
			args = append(args, "--scope", *c.Scope)
		}
		if c.Access != "" {
			args = append(args, "--access", c.Access)
		}
		if c.TTL != 0 {
			args = append(args, "--ttl", strconv.Itoa(c.TTL))
		}
		got, err := describe(t, helper, c.Vault, args...)
		if err != nil {
			t.Errorf("%s %q: %v\n%s", c.Vault, args, err, got)
		} else if got != c.Want {
			t.Errorf("%s %q:\n got  %q\n want %q", c.Vault, args, got, c.Want)
		}
	}
}

// A signing review's prompt says it signs, names the document as the vault
// reports it (made harmless), and never understates what the grant does.
func TestTouchIDHelper_SignPromptWording(t *testing.T) {
	helper := buildDebugHelper(t)
	for _, c := range loadPromptFixture(t).Signs {
		got, err := describe(t, helper, c.Vault, "describe-sign", c.Name, strconv.Itoa(c.Pages), c.Hash8, c.Requester)
		if err != nil {
			t.Errorf("%q: %v\n%s", c.Name, err, got)
		} else if got != c.Want {
			t.Errorf("%q:\n got  %q\n want %q", c.Name, got, c.Want)
		}
	}
}

func TestTouchIDHelper_RejectsOddScopes(t *testing.T) {
	helper := buildDebugHelper(t)
	f := loadPromptFixture(t)
	for _, scope := range f.RefusedScopes {
		if out, err := describe(t, helper, "", "describe-grant", "--scope", scope); err == nil {
			t.Errorf("scope %q accepted: %s", scope, out)
		}
	}
	for _, scope := range f.AcceptedScopes {
		if out, err := describe(t, helper, "", "describe-grant", "--scope", scope); err != nil {
			t.Errorf("scope %q refused: %v\n%s", scope, err, out)
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

// The helper parses scopes as the server does: every valid scope of the
// shared fixture gets a prompt, every invalid one is refused.
func TestTouchIDHelper_ScopeFixture(t *testing.T) {
	helper := buildDebugHelper(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "vault", "testdata", "scopes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ ValidScopes, InvalidScopes []string }
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	for _, scope := range f.ValidScopes {
		if out, err := exec.Command(helper, "describe-grant", "--scope", scope).CombinedOutput(); err != nil {
			t.Errorf("valid scope %q refused: %v\n%s", scope, err, out)
		}
	}
	for _, scope := range f.InvalidScopes {
		if out, err := exec.Command(helper, "describe-grant", "--scope", scope).CombinedOutput(); err == nil {
			t.Errorf("invalid scope %q accepted: %s", scope, out)
		}
	}
}
