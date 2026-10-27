package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// fakeTouchID installs a shell script as pvault-touchid next to the test
// binary. It records its arguments, answers status with enrolled for addr,
// and handles grant by exiting with grantExit (printing a token on 0).
func fakeTouchID(t *testing.T, addr string, grantExit int) (argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Touch ID is macOS only, and the fake helper is a shell script")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(exe), touchIDHelperName)
	argsFile = filepath.Join(t.TempDir(), "args")
	script := `#!/bin/sh
if [ "$1" = status ]; then echo enrolled; echo "server ` + addr + `"; exit 0; fi
echo "$@" >> "` + argsFile + `"
code=` + strconv.Itoa(grantExit) + `
[ $code = 0 ] && echo fake-token
exit $code
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	resetTouchID(t)
	return argsFile
}

func resetTouchID(t *testing.T) {
	touchIDCached = nil
	cliGrant.scope, cliGrant.access = "", ""
	t.Cleanup(func() {
		touchIDCached = nil
		cliGrant.scope, cliGrant.access = "", ""
	})
}

func TestTouchIDGrant_PassesCommandScope(t *testing.T) {
	t.Setenv("VAULT_ADDR", "")
	argsFile := fakeTouchID(t, "http://127.0.0.1:7200", 0)
	if !touchIDEnrolled() {
		t.Fatal("fake helper should count as enrolled")
	}

	needGrant("spid.password", "read")
	token, err := touchIDGrant()
	if err != nil || token != "fake-token" {
		t.Fatalf("grant = %q, %v", token, err)
	}
	cliGrant.scope, cliGrant.access = "", ""
	if _, err := touchIDGrant(); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(argsFile)
	want := "grant --scope spid.password --access read\ngrant\n"
	if string(data) != want {
		t.Fatalf("helper args:\n%s\nwant:\n%s", data, want)
	}
}

func TestTouchIDGrant_ExitCodes(t *testing.T) {
	fakeTouchID(t, "http://127.0.0.1:7200", touchIDDenied)
	if _, err := touchIDGrant(); err != errTouchIDDenied {
		t.Fatalf("denied: got %v", err)
	}
	fakeTouchID(t, "http://127.0.0.1:7200", touchIDNotEnrolled)
	if _, err := touchIDGrant(); err == nil || !strings.Contains(err.Error(), "touchid enable") {
		t.Fatalf("not enrolled: got %v", err)
	}
	fakeTouchID(t, "http://127.0.0.1:7200", 1)
	if _, err := touchIDGrant(); err == nil {
		t.Fatal("helper failure should be an error")
	}
}

// The helper never talks TCP: it sends the password only over this vault's
// socket. So another VAULT_ADDR (a named vault's port) doesn't turn Touch ID
// off; checkVaultServer, run before every prompt, refuses a VAULT_ADDR that
// isn't the verified server's (TestCheckVaultServer_RefusesOtherAddress).
func TestTouchIDEnrolled_AnyServerAddress(t *testing.T) {
	fakeTouchID(t, "http://127.0.0.1:7200", 0)
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:7299")
	if !touchIDEnrolled() {
		t.Fatal("Touch ID should not depend on VAULT_ADDR")
	}
}
