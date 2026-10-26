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
}

var touchIDCached *touchIDInfo

// touchIDStatus asks the helper once per process whether it holds a password
// for this vault (VAULT_DIR, which the helper reads too).
func touchIDStatus() touchIDInfo {
	if touchIDCached != nil {
		return *touchIDCached
	}
	var st touchIDInfo
	if helper, ok := touchIDHelper(); ok {
		st.helper = helper
		out, _ := exec.Command(helper, "status").Output()
		for _, line := range strings.Split(string(out), "\n") {
			if line == "enrolled" {
				st.enrolled = true
			}
		}
	}
	touchIDCached = &st
	return st
}

// touchIDEnrolled reports whether Touch ID can serve this command: the
// helper holds a password for this vault. The helper sends it only over this
// vault's socket, and the token it returns goes to serverAddr() over TCP,
// which checkVaultServer (run before every prompt) has checked is the server
// on that socket.
func touchIDEnrolled() bool {
	return touchIDStatus().enrolled
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
			return "", fmt.Errorf("Touch ID is not set up — run '%s'", pvCmd("touchid enable"))
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
