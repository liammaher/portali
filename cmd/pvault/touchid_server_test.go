//go:build darwin

package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
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

// pvault --vault <name> unlock with Touch ID starts and unlocks that vault's
// server, never the default vault's: --vault reaches the server it starts
// and the helper (which find the vault only through VAULT_DIR), and each
// vault listens on the port in its own port file.
func TestTouchIDHelper_UnlocksNamedVault(t *testing.T) {
	helper := buildDebugHelper(t)
	pvault := buildPvault(t, true)
	// pvault looks for the helper next to itself.
	data, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(pvault), touchIDHelperName), data, 0o755); err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp("", "pvh") // short: socket paths are limited to 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })

	freePort := func() int {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	ports := map[string]int{}
	for name, kind := range map[string]string{".pvault": vault.KindPersona, ".pvault-studio": vault.KindImpresa} {
		dir := filepath.Join(home, name)
		if _, err := vault.InitWithOptions(dir, "test-password", vault.InitOptions{Kind: kind}); err != nil {
			t.Fatal(err)
		}
		ports[name] = freePort()
		if err := writePort(dir, ports[name]); err != nil {
			t.Fatal(err)
		}
	}

	run := func(args ...string) (string, error) {
		cmd := exec.Command(pvault, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "VAULT_DIR=", "VAULT_ADDR=", "VAULT_PORT=", "PVAULT_VAULT=",
			"PVAULT_TOUCHID_TEST_APPROVE=1", "PVAULT_TOUCHID_TEST_PASSWORD=test-password", "PVAULT_TOUCHID_TEST_SERVER="+pvault)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	t.Cleanup(func() { run("--vault", "studio", "lock") })
	out, err := run("--vault", "studio", "unlock")
	if err != nil || !strings.Contains(out, "Touch ID") || !strings.Contains(out, strconv.Itoa(ports[".pvault-studio"])) {
		t.Fatalf("unlock: %v\n%s", err, out)
	}

	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(ports[".pvault-studio"]) + "/vault/status")
	if err != nil {
		t.Fatal(err)
	}
	var status vault.VaultStatus
	err = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if err != nil || status.Locked || status.Kind != vault.KindImpresa {
		t.Fatalf("studio vault status = %+v, %v", status, err)
	}
	if resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(ports[".pvault"]) + "/vault/status"); err == nil {
		resp.Body.Close()
		t.Fatal("the default vault's server was started too")
	}
	if out, err := run("status"); err != nil || !strings.Contains(out, "locked (server not running") {
		t.Fatalf("default vault status: %v\n%s", err, out)
	}
}

// The helper's hints name the vault they are about, as pvault's do: a named
// vault in the home folder gets --vault, any other VAULT_DIR is reached
// through VAULT_DIR itself.
func TestTouchIDHelper_HintsNameVault(t *testing.T) {
	helper := buildDebugHelper(t)
	pvault := buildPvault(t, true)
	home := t.TempDir()
	for dir, want := range map[string]string{
		filepath.Join(home, ".pvault-studio"):         "run 'pvault --vault studio unlock'",
		filepath.Join(home, ".pvault"):                "run 'pvault unlock'",
		filepath.Join(home, "elsewhere", ".pvault-x"): "run 'pvault unlock'",
	} {
		cmd := exec.Command(helper, "unlock")
		cmd.Env = append(os.Environ(), "HOME="+home, "VAULT_DIR="+dir,
			"PVAULT_TOUCHID_TEST_APPROVE=1", "PVAULT_TOUCHID_TEST_PASSWORD=test-password", "PVAULT_TOUCHID_TEST_SERVER="+pvault)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), want) {
			t.Errorf("%s: %v\n%s\nwant %q", dir, err, out, want)
		}
	}
}
