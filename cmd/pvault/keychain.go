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

var keychainServicePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// checkKeychain validates a --keychain service name before a token is created,
// so an unusable flag never leaves an orphaned token behind.
func checkKeychain(service string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("--keychain needs the macOS Keychain")
	}
	if !keychainServicePattern.MatchString(service) {
		return fmt.Errorf("--keychain service name must be 1-64 letters, digits, '.', '_' or '-'")
	}
	if _, err := exec.LookPath("security"); err != nil {
		return fmt.Errorf("--keychain: macOS 'security' tool not found")
	}
	return nil
}

// storeInKeychain saves token as a generic password for the current user under
// service, replacing any previous one. The token goes to `security -i` on stdin,
// never on a command line where other processes could see it, and is read
// back to confirm it was stored.
func storeInKeychain(service, token string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	if !keychainServicePattern.MatchString(u.Username) {
		return fmt.Errorf("unsupported user name %q", u.Username)
	}

	add := exec.Command("security", "-i")
	add.Stdin = strings.NewReader(fmt.Sprintf(
		"add-generic-password -U -a %s -s %s -w %s\n", u.Username, service, token))
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("security: %v: %s", err, bytes.TrimSpace(out))
	}

	got, err := exec.Command("security", "find-generic-password", "-a", u.Username, "-s", service, "-w").Output()
	if err != nil {
		return fmt.Errorf("could not read the token back from the Keychain: %v", err)
	}
	if strings.TrimSpace(string(got)) != token {
		return fmt.Errorf("the Keychain returned a different value than was stored")
	}
	return nil
}
