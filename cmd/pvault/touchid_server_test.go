//go:build darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
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

// The helper's sign command against a genuine server: the prompt is worded
// from what the vault says about the request, and the grant it prints reads
// and approves that request and opens nothing else.
func TestTouchIDHelper_SignGrant(t *testing.T) {
	helper := buildDebugHelper(t)
	shortVaultDir(t)
	pvault := buildPvault(t, true)
	startServe(t, pvault)
	if out, err := runDebugHelper(t, helper, pvault, "unlock"); err != nil {
		t.Fatalf("unlock: %v\n%s", err, out)
	}
	// The server's TCP address, as it reports it over its socket.
	sock := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath())
	}}}
	resp, err := sock.Get("http://localhost/vault/status")
	if err != nil {
		t.Fatal(err)
	}
	var status struct{ Addr string }
	json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	addr := "http://" + status.Addr
	call := func(method, path, token, contentType string, body []byte) (int, http.Header, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, addr+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, data
	}
	asJSON := func(v any) []byte { b, _ := json.Marshal(v); return b }

	_, _, data := call("POST", "/vault/session", "", "application/json", asJSON(map[string]string{"password": "test-password"}))
	var session struct{ Token string }
	json.Unmarshal(bytes.ReplaceAll(data, []byte(`"token"`), []byte(`"Token"`)), &session)
	if session.Token == "" {
		t.Fatalf("no session grant: %s", data)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 200, 80))
	for x := 10; x < 190; x++ {
		img.Set(x, 40+(x%20)-10, color.NRGBA{0, 0, 150, 255})
	}
	var pngBytes bytes.Buffer
	png.Encode(&pngBytes, img)
	if st, _, body := call("POST", "/vault/documents?category=firma_autografa&name=firma.png", session.Token, "image/png", pngBytes.Bytes()); st != 200 {
		t.Fatalf("store signature: %d %s", st, body)
	}
	st, _, data := call("POST", "/vault/signing", session.Token, "application/json", asJSON(map[string]any{
		"name": "mod-1.pdf", "purpose": "test", "pdf": signTestPDF(),
		"placements": []map[string]any{{"page": 1, "rect": []float64{100, 40, 190, 90}}},
	}))
	var info vault.SigningInfo
	if st != 200 || json.Unmarshal(data, &info) != nil {
		t.Fatalf("request: %d %s", st, data)
	}

	out, err := runDebugHelper(t, helper, pvault, "sign", "--id", info.ID)
	if err != nil {
		t.Fatalf("sign: %v\n%s", err, out)
	}
	if want := `would ask to review and sign "mod-1.pdf" (1 page, hash ` + info.SHA256[:8] + `), filed by session`; !strings.Contains(out, want) {
		t.Fatalf("the prompt wasn't worded from the request, want %q in:\n%s", want, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	grant := lines[len(lines)-1]

	if st, _, _ := call("GET", "/vault/fields", grant, "application/json", nil); st != 403 {
		t.Errorf("the sign grant listed fields: %d", st)
	}
	st, hdr, file := call("GET", "/vault/signing/"+info.ID+"/candidate", grant, "application/json", nil)
	if st != 200 || hdr.Get("X-Signing-Sha256") != info.SHA256 || len(file) == 0 {
		t.Fatalf("candidate: %d", st)
	}
	if st, _, body := call("POST", "/vault/signing/"+info.ID+"/approve", grant, "application/json", asJSON(map[string]string{"sha256": info.SHA256})); st != 200 {
		t.Fatalf("approve: %d %s", st, body)
	}

	// Approved, the request can't be reviewed again; a made-up one can't either.
	if out, err := runDebugHelper(t, helper, pvault, "sign", "--id", info.ID); err == nil || !strings.Contains(out, "no longer waiting") {
		t.Errorf("a decided request: err=%v\n%s", err, out)
	}
	if out, err := runDebugHelper(t, helper, pvault, "sign", "--id", strings.Repeat("a", 32)); err == nil {
		t.Errorf("an unknown request: %s", out)
	}
	if out, err := runDebugHelper(t, helper, pvault, "sign", "--id", "../etc"); err == nil || !strings.Contains(out, "usage") {
		t.Errorf("a malformed id: err=%v\n%s", err, out)
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
