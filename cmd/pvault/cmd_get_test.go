package main

import (
	"context"
	"encoding/json"
	"fmt"
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

	if ids, err := parseGetIDs([]string{"leo:cie.numero", "cie.numero"}); err != nil || len(ids) != 2 {
		t.Fatalf("member IDs: %v %v", ids, err)
	}
	if ids, err := parseGetIDs([]string{"residenza.*", "leo:identity.*", "*:identity.*", "cie.numero"}); err != nil || len(ids) != 4 {
		t.Fatalf("category patterns: %v %v", ids, err)
	}
	for _, bad := range []string{"*", "*:*", "io:cie.*", "Leo:cie.*", "*:leo:cie.*", "cie.*.*", ".*", "nodot", "a.b,c.d", "leo:*", "leo:", ":cie.numero", "io:cie.numero", "Leo:cie.numero", "a:b:c.d"} {
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
		fmt.Fprintf(w, `{"locked":false,"addr":%q}`, serverHost())
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
	serveOnSocket(t, serveHandler(t, mux))
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
	ids, values, err := getFields(ids, true)
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

	if _, _, err := getFields([]string{"identity.missing"}, false); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want not found", err)
	}
}

// touchIDVault starts a real vault and API server holding fields, and
// installs a stand-in helper that does what pvault-touchid does after a
// fingerprint: it asks the server for the grant pvault requested (scoped,
// or full access without --scope) and prints only the token. Each run's
// arguments are appended to the returned file.
func touchIDVault(t *testing.T, fields map[string]string) (*vault.Vault, string) {
	t.Helper()
	return touchIDVaultKind(t, vault.KindPersona, fields)
}

// touchIDVaultKind is touchIDVault for a vault of the given kind.
func touchIDVaultKind(t *testing.T, kind string, fields map[string]string) (*vault.Vault, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".vault")
	sk, err := vault.InitWithOptions(dir, "test-password", vault.InitOptions{Kind: kind})
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
			t.Fatalf("set %s: %v", id, err)
		}
	}

	srv := api.New(v, "127.0.0.1:0")
	ln, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Stop(context.Background()) })
	addr := "http://" + ln.Addr().String()
	t.Setenv("VAULT_ADDR", addr)
	serveOnSocket(t, srv.Serve)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(t.TempDir(), "args")
	script := `#!/bin/sh
if [ "$1" = status ]; then echo enrolled; echo "server ` + addr + `"; exit 0; fi
echo "$@" >> "` + argsFile + `"
[ "$1" = grant ] || exit 1
if [ "$2" = --scope ]; then
  [ "$4" = --access ] || exit 1
  body="{\"password\":\"test-password\",\"scope\":\"$3\",\"access\":\"$5\",\"ttl\":\"120s\"}"
else
  body='{"password":"test-password","ttl":"120s"}'
fi
/usr/bin/curl -sf -H 'Content-Type: application/json' -d "$body" ` + addr + `/vault/session | sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
`
	helper := filepath.Join(filepath.Dir(exe), touchIDHelperName)
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(helper) })
	resetTouchID(t)
	cliToken = ""
	t.Cleanup(func() { cliToken = ""; needGrant("", "") })
	return v, argsFile
}

// TestGet_EndToEnd runs multi-field get against a real vault and API server,
// so it checks the scope pvault sends is accepted, covers every field,
// reaches personal ones, and nothing else.
func TestGet_EndToEnd(t *testing.T) {
	v, argsFile := touchIDVault(t, map[string]string{
		"identity.full_name":   "Mario Rossi",
		"addresses.home_city":  "Napoli & dintorni",
		"spid.password":        "s3cret",
		"identity.nationality": "italiana",
	})
	if meta, _ := v.FieldMeta("spid.password"); meta == nil || meta.Sensitivity != vault.TierPersonal {
		t.Fatalf("spid.password should be personal, got %+v", meta)
	}

	ids, err := parseGetIDs([]string{"identity.full_name", "addresses.home_city", "spid.password", "identity.missing"})
	if err != nil {
		t.Fatal(err)
	}
	ids, values, err := getFields(ids, true)
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

// A form's fields come from a few whole categories: one get, one approval,
// each category's fields in its place, and its personal fields left out
// unless named.
func TestGet_CategoryPatterns(t *testing.T) {
	_, argsFile := touchIDVault(t, map[string]string{
		"identity.last_name":        "Rossi",
		"identity.first_name":       "Mario",
		"leo:identity.first_name":   "Leo",
		"giulia:identity.last_name": "Bianchi",
		"cie.numero":                "CA00000AA",
		"cie.pin":                   "12345678",
		"residenza.comune":          "Napoli",
	})
	args := []string{"cie.*", "*:identity.*", "identity.missing", "cie.pin"}
	ids, values, err := getFields(args, true)
	if err != nil {
		t.Fatal(err)
	}
	got := formatGetJSON(ids, values)
	want := "{\n" +
		"  \"cie.numero\": \"CA00000AA\",\n" +
		"  \"giulia:identity.last_name\": \"Bianchi\",\n" +
		"  \"identity.first_name\": \"Mario\",\n" +
		"  \"identity.last_name\": \"Rossi\",\n" +
		"  \"leo:identity.first_name\": \"Leo\",\n" +
		"  \"identity.missing\": null,\n" +
		"  \"cie.pin\": \"12345678\"\n" +
		"}"
	if got != want {
		t.Fatalf("output:\n%s\nwant:\n%s", got, want)
	}
	data, _ := os.ReadFile(argsFile)
	if string(data) != "grant --scope cie.*,*:identity.*,identity.missing,cie.pin --access read\n" {
		t.Fatalf("helper runs:\n%s", data)
	}

	// One person's category, without the PIN: it isn't named.
	cliToken = ""
	ids, values, err = getFields([]string{"leo:identity.*", "cie.*"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := formatGetJSON(ids, values); got != "{\n  \"leo:identity.first_name\": \"Leo\",\n  \"cie.numero\": \"CA00000AA\"\n}" {
		t.Fatalf("output:\n%s", got)
	}
}

// A scoped Touch ID grant can't do what only the user's own session may;
// pvault then asks once more, for full access, instead of failing.
func TestTouchIDFallsBackToFullAccessForSessionOnly(t *testing.T) {
	v, argsFile := touchIDVault(t, nil)
	if err := v.Set("identity.full_name", "Mario Rossi", "critical"); err != nil {
		t.Fatal(err)
	}

	needGrant("identity.full_name", "write")
	resp, err := apiRequest("DELETE", "/vault/fields/identity.full_name", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := apiResult(resp, nil); err != nil {
		t.Fatalf("delete of a critical field: %v", err)
	}
	if f, _ := v.FieldMeta("identity.full_name"); f != nil {
		t.Fatal("field still there")
	}

	// A document upload below the default tier, resent from the file's start.
	path := filepath.Join(t.TempDir(), "r.pdf")
	os.WriteFile(path, []byte("%PDF-1.4 receipt"), 0o600)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cliToken = ""
	needGrant("ricevute.documents", "append")
	resp, err = apiRawRequest("POST", "/vault/documents?category=ricevute&name=r.pdf&sensitivity=standard", "application/pdf", f)
	if err != nil {
		t.Fatal(err)
	}
	var doc vault.DocumentInfo
	if err := apiResult(resp, &doc); err != nil || doc.Size != int64(len("%PDF-1.4 receipt")) {
		t.Fatalf("upload: %+v, %v", doc, err)
	}

	data, _ := os.ReadFile(argsFile)
	want := "grant --scope identity.full_name --access write\ngrant\ngrant --scope ricevute.documents --access append\ngrant\n"
	if string(data) != want {
		t.Fatalf("helper runs:\n%s\nwant:\n%s", data, want)
	}
}
