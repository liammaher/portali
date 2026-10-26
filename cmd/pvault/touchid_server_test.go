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
	// Owner and family-member scopes are both accepted by the real server.
	for _, scope := range []string{"identity.*", "leo:*", "*:identity.*,leo:cie.numero"} {
		out, err = runDebugHelper(t, helper, pvault, "grant", "--scope", scope)
		if err != nil {
			t.Fatalf("grant %s: %v\n%s", scope, err, out)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if token := lines[len(lines)-1]; len(token) < 32 || strings.Contains(token, " ") {
			t.Fatalf("grant %s printed %q, want a token", scope, out)
		}
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
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"enroll"}, "usage:"},
		{[]string{"enroll", "--server", "pvault"}, "usage:"},
		{[]string{"enroll", "--server", plain}, "not signed with the hardened runtime"},
		{[]string{"enroll", "--server", "/nonexistent/pvault"}, "not a validly signed pvault"},
	} {
		cmd := exec.Command(helper, c.args...)
		cmd.Stdin = strings.NewReader("test-password\n")
		if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), c.want) {
			t.Errorf("%q: err = %v, want %q in:\n%s", c.args, err, c.want, out)
		}
	}
}

// Only the user at a terminal may change the enrollment: run without one,
// as an agent would, remove and enroll refuse before touching the Keychain.
func TestTouchIDHelper_EnrollmentNeedsTerminal(t *testing.T) {
	helper := buildDebugHelper(t)
	server := buildPvault(t, true)
	for _, args := range [][]string{{"remove"}, {"enroll", "--server", server}} {
		cmd := exec.Command(helper, args...)
		cmd.Env = append(os.Environ(), "VAULT_DIR="+t.TempDir()) // never the real Keychain item
		cmd.Stdin = strings.NewReader("test-password\n")
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "only works in a terminal") {
			t.Errorf("%q without a terminal: err %v, output %s", args, err, out)
		}
	}
}
