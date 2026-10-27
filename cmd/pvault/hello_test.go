package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/api"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

// fakeHello stands in for Windows Hello in the vault server.
type fakeHello struct {
	mu       sync.Mutex
	answer   bool
	messages []string
}

func (f *fakeHello) Available() error { return nil }

func (f *fakeHello) Approve(ctx context.Context, window uint64, message string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, message)
	return f.answer, nil
}

func (f *fakeHello) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.messages...)
}

// helloVault is a vault served on its socket with Windows Hello on and a fake
// in its place, and this CLI using it as on Windows.
func helloVault(t *testing.T, fields map[string]string) (*vault.Vault, *fakeHello) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".vault")
	sk, err := vault.Init(dir, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	if _, err := v.Unlock("test-password", sk); err != nil {
		t.Fatal(err)
	}
	for id, value := range fields {
		if err := v.Set(id, value, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.SetHelloEnabled(true); err != nil {
		t.Fatal(err)
	}
	f := &fakeHello{answer: true}
	srv := api.New(v, "127.0.0.1:0")
	srv.SetApprover(f)
	ln, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Stop(context.Background()) })
	t.Setenv("VAULT_ADDR", "http://"+ln.Addr().String())
	serveOnSocket(t, srv.Serve)

	old := currentApprover
	currentApprover = func() approver { return helloApprover{} }
	t.Cleanup(func() {
		currentApprover = old
		verifiedApprover, cliToken = "", ""
		needGrant("", "")
	})
	return v, f
}

// pvault get asks Windows Hello for exactly the field it reads, and reads it.
func TestHello_GetAsksForExactlyWhatItReads(t *testing.T) {
	_, f := helloVault(t, map[string]string{"identity.full_name": "Mario Rossi", "residenza.cap": "80100"})
	out := runCmd(t, cmdGet, "get", "identity.full_name")
	if !strings.Contains(out, "Mario Rossi") {
		t.Fatalf("get printed %q", out)
	}
	want := "pvault wants to read identity.full_name from your vault for 2 minutes."
	if msgs := f.asked(); len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("prompt %q, want %q", msgs, want)
	}
}

// Without a terminal there is no password to fall back on: a refusal, or
// Windows Hello turned off, is the answer.
func TestHello_RefusedOrOffWithoutTerminal(t *testing.T) {
	v, f := helloVault(t, nil)
	f.answer = false
	needGrant("cie.*", "read")
	if _, err := promptGrant("2m"); !errors.Is(err, errHelloDenied) {
		t.Fatalf("refused: %v", err)
	}
	if err := v.SetHelloEnabled(false); err != nil {
		t.Fatal(err)
	}
	_, err := promptGrant("2m")
	if err == nil || !strings.Contains(err.Error(), "Windows Hello") || !strings.Contains(err.Error(), "hello enable") {
		t.Fatalf("off: %v", err)
	}
	if len(f.asked()) != 1 {
		t.Fatalf("asked %d times", len(f.asked()))
	}
}

// A server that grants other than what was asked is not believed: its token
// goes unused.
func TestHello_RefusesGrantOtherThanAsked(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vault/session", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"token":"t","scope":"*","access":"write","sign":"other"}`))
	})
	serveOnSocket(t, serveHandler(t, mux))
	t.Cleanup(func() { needGrant("", "") })
	needGrant("cie.*", "read")
	if _, err := (helloApprover{}).grant(); err == nil || !strings.Contains(err.Error(), "other than what was approved") {
		t.Fatalf("grant: %v", err)
	}
	if _, err := (helloApprover{}).sign(strings.Repeat("a", 32)); err == nil {
		t.Fatal("sign: a grant for another request was taken")
	}
}

// A server that has Windows Hello off while this command thinks it's on says
// so, naming the command that turns it on.
func TestHello_ServerSaysOff(t *testing.T) {
	v, _ := helloVault(t, nil)
	if err := v.SetHelloEnabled(false); err != nil {
		t.Fatal(err)
	}
	needGrant("cie.*", "read")
	_, err := (helloApprover{}).grant()
	if err == nil || !strings.Contains(err.Error(), "off for this vault") || !strings.Contains(err.Error(), "hello enable") {
		t.Fatalf("err = %v", err)
	}
}
