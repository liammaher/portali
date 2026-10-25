package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"os/user"
	"regexp"
	"runtime"
	"strings"
)

// Names are written unquoted into a `security -i` command line, so they must
// not contain separators or start with '-' (which security would parse as an
// option, and which usually means a flag value was forgotten).
var keychainNamePattern = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]{0,63}$`)

// keychainAccount is the Keychain account the token is stored under: the
// current macOS user.
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
	if _, err := exec.LookPath("security"); err != nil {
		return fmt.Errorf("--keychain: macOS 'security' tool not found")
	}
	_, err := keychainAccount()
	return err
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

	add := exec.Command("security", "-i")
	add.Stdin = strings.NewReader(fmt.Sprintf(
		"add-generic-password -U -a %s -s %s -w %s\n", account, service, token))
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("security: %v: %s", err, bytes.TrimSpace(out))
	}

	got, err := exec.Command("security", "find-generic-password", "-a", account, "-s", service, "-w").Output()
	if err != nil {
		return fmt.Errorf("could not read the token back from the Keychain: %v", err)
	}
	if strings.TrimSpace(string(got)) != token {
		return fmt.Errorf("the Keychain returned a different value than was stored")
	}
	return nil
}
