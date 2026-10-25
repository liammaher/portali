package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/api"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

func TestParseGetIDs(t *testing.T) {
	ids, err := parseGetIDs([]string{"identity.full_name", "cie.numero", "identity.full_name"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "identity.full_name,cie.numero" {
		t.Fatalf("ids = %v, want duplicates dropped in order", ids)
	}

	for _, bad := range []string{"residenza.*", "*", "nodot", "a.b,c.d"} {
		if _, err := parseGetIDs([]string{bad}); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}

	var many []string
	for i := 0; i <= maxGetFields; i++ {
		many = append(many, "identity.f"+string(rune('a'+i)))
	}
	if _, err := parseGetIDs(many); err == nil {
		t.Fatalf("%d fields should exceed the limit", len(many))
	}
}

// fakeVaultServer serves /vault/status and the given fields to fake-token.
func fakeVaultServer(t *testing.T, fields map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vault/status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"locked":false}`))
	})
	mux.HandleFunc("GET /vault/fields/{id...}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		id := r.PathValue("id")
		value, ok := fields[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"field not found"}`))
			return
		}
		json.NewEncoder(w).Encode(vault.FieldInfo{ID: id, Value: value})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGetFields_OneTouchIDPromptForAll(t *testing.T) {
	srv := fakeVaultServer(t, map[string]string{
		"identity.full_name": "Mario Rossi",
		"cie.numero":         "CA00000AA",
	})
	t.Setenv("VAULT_ADDR", srv.URL)
	argsFile := fakeTouchID(t, srv.URL, 0)
	cliToken = ""
	t.Cleanup(func() { cliToken = "" })

	ids := []string{"identity.full_name", "cie.numero", "identity.missing"}
	values, err := getFields(ids, true)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(argsFile)
	want := "grant --scope identity.full_name,cie.numero,identity.missing --access read\n"
	if string(data) != want {
		t.Fatalf("helper calls:\n%s\nwant one:\n%s", data, want)
	}

	got := formatGetJSON(ids, values)
	wantJSON := "{\n  \"identity.full_name\": \"Mario Rossi\",\n  \"cie.numero\": \"CA00000AA\",\n  \"identity.missing\": null\n}"
	if got != wantJSON {
		t.Fatalf("output:\n%s\nwant:\n%s", got, wantJSON)
	}
}

// A single missing field is still an error, as before multi-field get.
func TestGetFields_SingleMissingIsError(t *testing.T) {
	srv := fakeVaultServer(t, nil)
	t.Setenv("VAULT_ADDR", srv.URL)
	fakeTouchID(t, srv.URL, 0)
	cliToken = ""
	t.Cleanup(func() { cliToken = "" })

	if _, err := getFields([]string{"identity.missing"}, false); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want not found", err)
	}
}

// TestGet_EndToEnd runs multi-field get against a real vault and API server.
// The stand-in helper does what pvault-touchid does after a fingerprint: it
// asks the server for a grant scoped to the requested fields and prints only
// the token. So this checks the scope pvault sends is accepted, covers every
// field, reaches personal ones, and nothing else.
func TestGet_EndToEnd(t *testing.T) {
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
	for id, value := range map[string]string{
		"identity.full_name":   "Mario Rossi",
		"addresses.home_city":  "Napoli & dintorni",
		"spid.password":        "s3cret",
		"identity.nationality": "italiana",
	} {
		if err := v.Set(id, value, ""); err != nil {
			t.Fatalf("set %s: %v", id, err)
		}
	}
	if meta, _ := v.FieldMeta("spid.password"); meta == nil || meta.Sensitivity != vault.TierPersonal {
		t.Fatalf("spid.password should be personal, got %+v", meta)
	}

	srv := api.New(v, "127.0.0.1:0")
	ln, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Stop(context.Background()) })
	addr := "http://" + ln.Addr().String()
	t.Setenv("VAULT_ADDR", addr)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(t.TempDir(), "args")
	script := `#!/bin/sh
if [ "$1" = status ]; then echo enrolled; echo "server ` + addr + `"; exit 0; fi
echo "$@" >> "` + argsFile + `"
[ "$1" = grant ] && [ "$2" = --scope ] && [ "$4" = --access ] || exit 1
/usr/bin/curl -sf -H 'Content-Type: application/json' \
  -d "{\"password\":\"test-password\",\"scope\":\"$3\",\"access\":\"$5\",\"ttl\":\"120s\"}" \
  ` + addr + `/vault/session | sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
`
	helper := filepath.Join(filepath.Dir(exe), touchIDHelperName)
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(helper) })
	resetTouchID(t)
	cliToken = ""
	t.Cleanup(func() { cliToken = "" })

	ids, err := parseGetIDs([]string{"identity.full_name", "addresses.home_city", "spid.password", "identity.missing"})
	if err != nil {
		t.Fatal(err)
	}
	values, err := getFields(ids, true)
	if err != nil {
		t.Fatal(err)
	}
	got := formatGetJSON(ids, values)
	want := "{\n" +
		"  \"identity.full_name\": \"Mario Rossi\",\n" +
		"  \"addresses.home_city\": \"Napoli & dintorni\",\n" +
		"  \"spid.password\": \"s3cret\",\n" +
		"  \"identity.missing\": null\n" +
		"}"
	if got != want {
		t.Fatalf("output:\n%s\nwant:\n%s", got, want)
	}
	var decoded map[string]*string
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	data, _ := os.ReadFile(argsFile)
	if n := strings.Count(string(data), "grant"); n != 1 {
		t.Fatalf("want exactly one approval, helper ran %d times:\n%s", n, data)
	}

	// The grant covers the requested fields and nothing else.
	resp, err := doRequest("GET", "/vault/fields/identity.nationality", nil, cliToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unrequested field: status %d, want 403", resp.StatusCode)
	}
	resp, err = doRequest("PUT", "/vault/fields/identity.full_name", map[string]string{"value": "x"}, cliToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("write with a read grant: status %d, want 403", resp.StatusCode)
	}
}
