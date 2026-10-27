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

	"github.com/lovincyrus/personal-vault/internal/wincred"
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

// hexTokenPattern matches a service token as the server makes them.
var hexTokenPattern = regexp.MustCompile(`^[0-9a-f]{1,128}$`)

// keychainName is where --keychain keeps a token on this system.
func keychainName() string {
	if runtime.GOOS == "windows" {
		return "Credential Manager"
	}
	return "Keychain"
}

// keychainAccount is the account the token is stored under: the current
// user, as the MCP server's VAULT_KEYCHAIN_SERVICE lookup names it (on
// Windows without the DOMAIN\ part, as Node and Python report it).
func keychainAccount() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return u.Username[strings.LastIndex(u.Username, `\`)+1:], nil
	}
	if !keychainNamePattern.MatchString(u.Username) {
		return "", fmt.Errorf("--keychain: unsupported user name %q", u.Username)
	}
	return u.Username, nil
}

// checkKeychain validates everything --keychain needs before a token is
// created, so an unusable flag never leaves an orphaned token behind.
func checkKeychain(service string) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return fmt.Errorf("--keychain needs the macOS Keychain or Windows Credential Manager")
	}
	if !keychainNamePattern.MatchString(service) {
		return fmt.Errorf("--keychain needs a service name of 1-64 letters, digits, '.', '_' or '-' (not starting with '-'), e.g. --keychain pvault-mcp")
	}
	if runtime.GOOS == "windows" {
		account, err := keychainAccount()
		if err != nil {
			return err
		}
		// Credential Manager keys on the name alone (the Keychain on name and
		// account): never overwrite what was saved for someone else, such as
		// a PEC password under a mistyped name.
		if _, user, err := wincred.Read(service); err == nil && !strings.EqualFold(user, account) {
			return fmt.Errorf("--keychain: %q in Credential Manager was saved for %q, not for you: choose another name", service, user)
		}
		return nil
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
	if runtime.GOOS == "windows" {
		account, err := keychainAccount()
		if err != nil {
			return "", err
		}
		secret, user, err := wincred.Read(service)
		// Saved for someone else counts as none, as a Keychain item of
		// another account would.
		if err == wincred.ErrNotFound || (err == nil && !strings.EqualFold(user, account)) {
			return "", nil
		}
		return secret, err
	}
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
	// The token comes from the server over TCP and is written into a
	// `security -i` command line, so anything but hex could add a command.
	if !hexTokenPattern.MatchString(token) {
		return errors.New("the server returned a malformed token; not storing it")
	}
	account, err := keychainAccount()
	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		if err := wincred.Write(service, account, token); err != nil {
			return err
		}
	} else if err := addGenericPassword(account, service, token); err != nil {
		return err
	}

	got, err := readKeychain(service)
	if err != nil {
		return fmt.Errorf("could not read the token back from the %s: %v", keychainName(), err)
	}
	if got != token {
		return fmt.Errorf("the %s returned a different value than was stored", keychainName())
	}
	return nil
}

// addGenericPassword saves value as the Keychain password of account under
// service, through `security -i` on stdin.
func addGenericPassword(account, service, value string) error {
	add := exec.Command(securityPath, "-i")
	add.Stdin = strings.NewReader(fmt.Sprintf(
		"add-generic-password -U -a %s -s %s -w %s\n", account, service, value))
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("security: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// tokenRef identifies a service token to the revoke endpoint by its hash, so
// the raw token never appears in a URL.
func tokenRef(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
