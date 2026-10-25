package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Touch ID support is a separate signed Swift helper (cmd/pvault-touchid)
// installed next to pvault. It keeps the profile password in the Keychain,
// asks for a fingerprint and talks to the server itself, so pvault only ever
// receives a scoped grant token — never the password.

// touchIDHelperName is looked up only next to the pvault binary, never on
// PATH, so a planted binary can't stand in for it.
const touchIDHelperName = "pvault-touchid"

// Helper exit statuses (see cmd/pvault-touchid/main.swift).
const (
	touchIDDenied      = 2
	touchIDNotEnrolled = 3
)

var errTouchIDDenied = errors.New("Touch ID request was not approved")

// cliGrant is what this command needs; a Touch ID approval covers exactly
// this. An empty scope asks for full access.
var cliGrant struct {
	scope  string
	access string
}

// needGrant records the scope and access this command needs, so a Touch ID
// prompt can name them instead of asking for full access.
func needGrant(scope, access string) {
	cliGrant.scope = scope
	cliGrant.access = access
}

func touchIDHelper() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	path := filepath.Join(filepath.Dir(exe), touchIDHelperName)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

type touchIDInfo struct {
	helper   string // path, or "" if not installed
	enrolled bool   // a password is saved for this vault
	addr     string // the only server the helper talks to
}

var (
	touchIDCached *touchIDInfo
	touchIDWarned bool
)

// touchIDStatus asks the helper once per process whether it holds a password
// for this vault and which server it uses.
func touchIDStatus() touchIDInfo {
	if touchIDCached != nil {
		return *touchIDCached
	}
	var st touchIDInfo
	if helper, ok := touchIDHelper(); ok {
		st.helper = helper
		// Exit status 3 (not enrolled) still prints the server line.
		out, _ := exec.Command(helper, "status").Output()
		for _, line := range strings.Split(string(out), "\n") {
			if line == "enrolled" {
				st.enrolled = true
			}
			if addr, ok := strings.CutPrefix(line, "server "); ok {
				st.addr = addr
			}
		}
	}
	touchIDCached = &st
	return st
}

// touchIDEnrolled reports whether Touch ID can serve this command: the
// helper holds a password for this vault and talks to the same server as
// pvault. The helper's server address is fixed, so with another VAULT_ADDR
// it would send this vault's password to the wrong server.
func touchIDEnrolled() bool {
	st := touchIDStatus()
	if !st.enrolled {
		return false
	}
	if st.addr != serverAddr() {
		if !touchIDWarned {
			touchIDWarned = true
			fmt.Fprintf(os.Stderr, "Touch ID skipped: it only works with the vault at %s, and VAULT_ADDR is %s.\n", st.addr, serverAddr())
		}
		return false
	}
	return true
}

// runTouchID runs the helper with its prompt on screen and errors on
// stderr, returning its trimmed stdout.
func runTouchID(stdin string, args ...string) (string, error) {
	helper, ok := touchIDHelper()
	if !ok {
		return "", fmt.Errorf("%s is not installed next to pvault — run 'make build'", touchIDHelperName)
	}
	cmd := exec.Command(helper, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin + "\n")
	}
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case touchIDDenied:
			return "", errTouchIDDenied
		case touchIDNotEnrolled:
			return "", errors.New("Touch ID is not set up — run 'pvault touchid enable'")
		}
		return "", fmt.Errorf("%s failed", touchIDHelperName)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// touchIDGrant asks for a fingerprint and returns a grant covering cliGrant.
func touchIDGrant() (string, error) {
	args := []string{"grant"}
	if cliGrant.scope != "" {
		args = append(args, "--scope", cliGrant.scope)
		if cliGrant.access != "" {
			args = append(args, "--access", cliGrant.access)
		}
	}
	token, err := runTouchID("", args...)
	if err == nil && token == "" {
		err = errors.New("Touch ID helper returned no token")
	}
	return token, err
}
