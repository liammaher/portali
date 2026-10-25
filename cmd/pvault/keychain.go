package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"runtime"
	"strings"
)

// securityPath is macOS's Keychain tool, run by absolute path so a binary
// earlier in PATH can't intercept the token.
const securityPath = "/usr/bin/security"

// securityItemNotFound is the exit status `security find-generic-password`
// uses when no matching item exists (errSecItemNotFound).
const securityItemNotFound = 44

// Names are written unquoted into a `security -i` command line, so they must
// not contain separators or start with '-' (which security would parse as an
// option, and which usually means a flag value was forgotten).
var keychainNamePattern = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]{0,63}$`)

// keychainAccount is the Keychain account the token is stored under: the
// current macOS user. The MCP server's VAULT_KEYCHAIN_SERVICE lookup uses
// the same account.
func keychainAccount() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	if !keychainNamePattern.MatchString(u.Username) {
		return "", fmt.Errorf("--keychain: unsupported user name %q", u.Username)
	}
	return u.Username, nil
}

// checkKeychain validates everything --keychain needs before a token is
// created, so an unusable flag never leaves an orphaned token behind.
func checkKeychain(service string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("--keychain needs the macOS Keychain")
	}
	if !keychainNamePattern.MatchString(service) {
		return fmt.Errorf("--keychain needs a service name of 1-64 letters, digits, '.', '_' or '-' (not starting with '-'), e.g. --keychain pvault-mcp")
	}
	if _, err := os.Stat(securityPath); err != nil {
		return fmt.Errorf("--keychain: %s not found", securityPath)
	}
	_, err := keychainAccount()
	return err
}

// readKeychain returns the token stored under service for the current user,
// or "" if there is none.
func readKeychain(service string) (string, error) {
	account, err := keychainAccount()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(securityPath, "find-generic-password", "-a", account, "-s", service, "-w").Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == securityItemNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// storeInKeychain saves token as a generic password for the current user under
// service, replacing any previous one. The token goes to `security -i` on stdin,
// never on a command line where other processes could see it, and is read
// back to confirm it was stored.
func storeInKeychain(service, token string) error {
	account, err := keychainAccount()
	if err != nil {
		return err
	}

	add := exec.Command(securityPath, "-i")
	add.Stdin = strings.NewReader(fmt.Sprintf(
		"add-generic-password -U -a %s -s %s -w %s\n", account, service, token))
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("security: %v: %s", err, bytes.TrimSpace(out))
	}

	got, err := readKeychain(service)
	if err != nil {
		return fmt.Errorf("could not read the token back from the Keychain: %v", err)
	}
	if got != token {
		return fmt.Errorf("the Keychain returned a different value than was stored")
	}
	return nil
}

// tokenRef identifies a service token to the revoke endpoint by its hash, so
// the raw token never appears in a URL.
func tokenRef(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
