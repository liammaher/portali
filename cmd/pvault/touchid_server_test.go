//go:build darwin

package main

import (
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

// runDebugHelper runs a debug helper as if enrolled with the test password
// and server, approving without a finger. No Keychain item is touched.
func runDebugHelper(t *testing.T, helper, server string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(helper, args...)
	cmd.Env = append(os.Environ(),
		"PVAULT_TOUCHID_TEST_APPROVE=1",
		"PVAULT_TOUCHID_TEST_PASSWORD=test-password",
		"PVAULT_TOUCHID_TEST_SERVER="+server)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestTouchIDHelper_UnlocksGenuineServer(t *testing.T) {
	helper := buildDebugHelper(t)
	shortVaultDir(t)
	pvault := buildPvault(t, true)
	startServe(t, pvault)

	out, err := runDebugHelper(t, helper, pvault, "unlock")
	if err != nil || !strings.HasSuffix(strings.TrimSpace(out), "unlocked") {
		t.Fatalf("unlock: %v\n%s", err, out)
	}
	out, err = runDebugHelper(t, helper, pvault, "grant", "--scope", "identity.*")
	if err != nil {
		t.Fatalf("grant: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if token := lines[len(lines)-1]; len(token) < 32 || strings.Contains(token, " ") {
		t.Fatalf("grant printed %q, want a token", out)
	}
}

// The helper never sends the password to anything but the enrolled pvault
// build: not a program impersonating it, not a different pvault build, and
// not an enrolled pvault without the hardened runtime.
func TestTouchIDHelper_RefusesImpostors(t *testing.T) {
	helper := buildDebugHelper(t)
	pvault := buildPvault(t, true)

	t.Run("other program", func(t *testing.T) {
		var got atomic.Int32
		serveOnSocket(t, serveHandler(t, echoMux(&got))) // this test binary
		out, err := runDebugHelper(t, helper, pvault, "unlock")
		if err == nil || !strings.Contains(out, "refusing to send your password") {
			t.Fatalf("err = %v\n%s", err, out)
		}
		if got.Load() != 0 {
			t.Fatal("the impostor received the password")
		}
	})

	t.Run("other pvault build", func(t *testing.T) {
		shortVaultDir(t)
		startServe(t, buildPvault(t, true, "-ldflags=-s"))
		out, err := runDebugHelper(t, helper, pvault, "unlock")
		if err == nil || !strings.Contains(out, "refusing to send your password: the vault server is") {
			t.Fatalf("err = %v\n%s", err, out)
		}
	})

	t.Run("not hardened", func(t *testing.T) {
		shortVaultDir(t)
		plain := buildPvault(t, false)
		startServe(t, plain)
		out, err := runDebugHelper(t, helper, plain, "unlock")
		if err == nil || !strings.Contains(out, "hardened runtime") {
			t.Fatalf("err = %v\n%s", err, out)
		}
	})
}

// Enrolling needs the absolute path of a hardened pvault. These all fail
// before the Keychain is touched.
func TestTouchIDHelper_EnrollChecksServer(t *testing.T) {
	helper := buildDebugHelper(t)
	plain := buildPvault(t, false)
	for _, args := range [][]string{
		{"enroll"},
		{"enroll", "--server", "pvault"},
		{"enroll", "--server", plain},
		{"enroll", "--server", "/nonexistent/pvault"},
	} {
		cmd := exec.Command(helper, args...)
		cmd.Stdin = strings.NewReader("test-password\n")
		if out, err := cmd.CombinedOutput(); err == nil || strings.Contains(string(out), "enrolled") {
			t.Errorf("%q accepted: %s", args, out)
		}
	}
}
