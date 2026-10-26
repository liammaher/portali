package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// TestPraticaPilotVault serves a synthetic vault for the pratica fill pilot
// (claude/skills/pratica/fill/pilot/run.sh), which drives it like an agent:
// over HTTP, with service tokens scoped as the pratica skill asks for them.
// It runs only with PVAULT_PILOT_DIR set. From that directory it reads
// fixture.json (the vault's kind and fields, set as the user would) and
// needs.json (the tokens to create), writes server.json (the address and the
// tokens), and serves until a file named done appears there.
func TestPraticaPilotVault(t *testing.T) {
	dir := os.Getenv("PVAULT_PILOT_DIR")
	if dir == "" {
		t.Skip("PVAULT_PILOT_DIR not set")
	}
	var fixture struct {
		Kind   string     `json:"kind"`
		Fields [][]string `json:"fields"` // id, value[, tier], in order: a person's first field adds them
	}
	var needs struct {
		Tokens map[string]struct {
			Scope  string `json:"scope"`
			Access string `json:"access"`
		} `json:"tokens"`
	}
	readJSON(t, filepath.Join(dir, "fixture.json"), &fixture)
	readJSON(t, filepath.Join(dir, "needs.json"), &needs)

	vdir := filepath.Join(t.TempDir(), ".pvault")
	sk, err := vault.InitWithOptions(vdir, testPassword, vault.InitOptions{Kind: fixture.Kind})
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(vdir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	if _, err := v.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixture.Fields {
		if len(f) < 2 {
			t.Fatalf("fixture field %v: want id, value[, tier]", f)
		}
		if err := v.Set(f[0], f[1], strings.Join(f[2:], "")); err != nil {
			t.Fatalf("fixture %s: %v", f[0], err)
		}
	}

	tokens := map[string]string{}
	for name, tok := range needs.Tokens {
		token, err := v.CreateServiceToken("pratica-pilot-"+name, tok.Scope, time.Hour, "", tok.Access)
		if err != nil {
			t.Fatalf("token %s (%s): %v", name, tok.Scope, err)
		}
		tokens[name] = token
	}

	srv := httptest.NewServer(New(v, ":0").handler)
	defer srv.Close()
	out, _ := json.Marshal(map[string]any{"url": srv.URL, "tokens": tokens})
	// Whole or not at all: run.sh starts as soon as the file is there.
	tmp := filepath.Join(dir, "server.json.tmp")
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "server.json")); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Minute); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(dir, "done")); err == nil {
			return
		}
	}
	t.Fatal("no done file after 10 minutes")
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
