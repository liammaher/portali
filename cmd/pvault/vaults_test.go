package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// isolatedHome gives the test its own $HOME with no vaults, and no vault
// selection from the environment.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VAULT_DIR", "")
	t.Setenv("PVAULT_VAULT", "")
	return home
}

func TestSelectVault(t *testing.T) {
	home := isolatedHome(t)
	args, err := selectVault([]string{"pvault", "--vault", "studio", "person", "list"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "pvault person list" {
		t.Errorf("args = %v", args)
	}
	want := filepath.Join(home, ".pvault-studio")
	if got := os.Getenv("VAULT_DIR"); got != want {
		t.Errorf("VAULT_DIR = %q, want %q", got, want)
	}
	if vaultName() != "studio" || vaultFlag() != "--vault studio " || pvCmd("unlock") != "pvault --vault studio unlock" {
		t.Errorf("name %q, flag %q, cmd %q", vaultName(), vaultFlag(), pvCmd("unlock"))
	}
	// Child processes (serve, the Touch ID helper) see the same vault.
	out, err := exec.Command("sh", "-c", `printf %s "$VAULT_DIR"`).Output()
	if err != nil || string(out) != want {
		t.Errorf("child VAULT_DIR = %q, %v", out, err)
	}

	// --vault=name, and PVAULT_VAULT, which is consumed.
	isolatedHome(t)
	if args, err := selectVault([]string{"pvault", "--vault=shop", "status"}); err != nil || len(args) != 2 {
		t.Errorf("--vault=shop: %v, %v", args, err)
	}
	isolatedHome(t)
	t.Setenv("PVAULT_VAULT", "studio")
	if _, err := selectVault([]string{"pvault", "status"}); err != nil || vaultName() != "studio" || os.Getenv("PVAULT_VAULT") != "" {
		t.Errorf("PVAULT_VAULT: %v, name %q", err, vaultName())
	}

	// No selection: the default vault, untouched.
	isolatedHome(t)
	if args, err := selectVault([]string{"pvault", "status"}); err != nil || len(args) != 2 || os.Getenv("VAULT_DIR") != "" || vaultName() != "" {
		t.Errorf("default: %v, %v", args, err)
	}
	// Only as the first argument: later it's the command's to judge.
	if args, _ := selectVault([]string{"pvault", "status", "--vault", "x"}); len(args) != 4 {
		t.Errorf("a later --vault was consumed: %v", args)
	}

	for _, bad := range [][]string{{"pvault", "--vault"}, {"pvault", "--vault", "", "status"}, {"pvault", "--vault=", "status"},
		{"pvault", "--vault", "Studio", "status"}, {"pvault", "--vault", "../x", "status"}} {
		isolatedHome(t)
		if _, err := selectVault(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	// A VAULT_DIR naming another directory conflicts; the same one is fine
	// (what a child process started with --vault sees).
	home = isolatedHome(t)
	t.Setenv("VAULT_DIR", filepath.Join(home, "elsewhere"))
	if _, err := selectVault([]string{"pvault", "--vault", "studio", "status"}); err == nil {
		t.Error("--vault with a different VAULT_DIR accepted")
	}
	t.Setenv("VAULT_DIR", filepath.Join(home, ".pvault-studio"))
	if _, err := selectVault([]string{"pvault", "--vault", "studio", "status"}); err != nil {
		t.Errorf("--vault with the same VAULT_DIR: %v", err)
	}
}

func TestPorts(t *testing.T) {
	home := isolatedHome(t)
	// The default vault keeps 7200, file or not.
	if p, err := pickPort(filepath.Join(home, ".pvault")); err != nil || p != defaultPort {
		t.Errorf("default vault port = %d, %v", p, err)
	}
	if got := vaultPort(); got != defaultPort {
		t.Errorf("vaultPort without a file = %d", got)
	}

	// Named vaults get the lowest port no other vault has and nothing uses.
	busy, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(firstNamedPort))
	if err != nil {
		t.Skipf("port %d in use: %v", firstNamedPort, err)
	}
	defer busy.Close()
	other := filepath.Join(home, ".pvault-other")
	os.MkdirAll(other, 0700)
	if err := writePort(other, firstNamedPort+1); err != nil {
		t.Fatal(err)
	}
	studio := filepath.Join(home, ".pvault-studio")
	os.MkdirAll(studio, 0700)
	p, err := ensurePort(studio)
	// Not 7201 (busy) nor 7202 (another vault's); the next free one, which
	// is 7203 unless something on this machine already listens there.
	if err != nil || p <= firstNamedPort+1 || p > lastNamedPort {
		t.Fatalf("studio port = %d, %v; want above %d", p, err, firstNamedPort+1)
	}
	// It is kept, and read back by serverAddr.
	if p2, _ := ensurePort(studio); p2 != p {
		t.Errorf("ensurePort changed the port: %d → %d", p, p2)
	}
	t.Setenv("VAULT_DIR", studio)
	t.Setenv("VAULT_ADDR", "")
	if got, want := serverAddr(), "http://127.0.0.1:"+strconv.Itoa(p); got != want {
		t.Errorf("serverAddr = %q, want %q", got, want)
	}
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:9999")
	if serverAddr() != "http://127.0.0.1:9999" {
		t.Error("VAULT_ADDR should win over the port file")
	}
	if info, err := os.Stat(portPath(studio)); err != nil || info.Mode().Perm() != 0600 {
		t.Errorf("port file mode = %v, %v", info.Mode().Perm(), err)
	}

	// A broken port file is an error, not a silent 7200.
	os.WriteFile(portPath(other), []byte("seven\n"), 0600)
	if _, _, err := readPort(other); err == nil {
		t.Error("a non-numeric port file was accepted")
	}
}
