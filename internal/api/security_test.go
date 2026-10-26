package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// createToken creates a 1h service token for consumer "agent" from req
// (scope plus any of access and max_sensitivity).
func createToken(t *testing.T, env *testEnv, req map[string]string) string {
	t.Helper()
	body := map[string]string{"consumer": "agent", "ttl": "1h"}
	for k, v := range req {
		body[k] = v
	}
	w := env.doRequest(t, "POST", "/vault/tokens/service", body, true)
	if w.Code != 200 {
		t.Fatalf("create token: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	return resp.Token
}

// createTokenWithAccess creates a service token with the given access level
// (read, append or write) and the default sensitivity cap.
func createTokenWithAccess(t *testing.T, env *testEnv, scope, access string) string {
	t.Helper()
	return createToken(t, env, map[string]string{"scope": scope, "access": access})
}

func createTokenWithTier(t *testing.T, env *testEnv, scope, maxSensitivity string) string {
	t.Helper()
	return createToken(t, env, map[string]string{"scope": scope, "max_sensitivity": maxSensitivity})
}

func seedTiers(t *testing.T, env *testEnv) {
	t.Helper()
	for id, tier := range map[string]string{
		"identity.full_name":      "standard",
		"identity.codice_fiscale": "sensitive",
		"payment.card_number":     "critical",
	} {
		value := "x"
		if id == "identity.codice_fiscale" {
			value = "CCCFBA85D03L219P"
		}
		w := env.doRequest(t, "PUT", "/vault/fields/"+id, map[string]string{"value": value, "sensitivity": tier}, true)
		if w.Code != 200 {
			t.Fatalf("seed %s: %d %s", id, w.Code, w.Body.String())
		}
	}
}

func TestServiceToken_DefaultTierBlocksCritical(t *testing.T) {
	env := setup(t)
	seedTiers(t, env)
	token := createScopedToken(t, env, "agent", "*") // no max_sensitivity: defaults to sensitive

	w := env.doRequestWithToken(t, "GET", "/vault/fields/payment.card_number", nil, token)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "sensitivity_exceeded") {
		t.Fatalf("critical field: expected 403 sensitivity_exceeded, got %d: %s", w.Code, w.Body.String())
	}
	w = env.doRequestWithToken(t, "GET", "/vault/fields/identity.codice_fiscale", nil, token)
	if w.Code != 200 {
		t.Fatalf("sensitive field: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = env.doRequestWithToken(t, "GET", "/vault/context", nil, token)
	var ctx vault.ContextBundle
	json.NewDecoder(w.Body).Decode(&ctx)
	if _, ok := ctx.Categories["payment"]; ok {
		t.Fatal("context leaked critical payment fields")
	}
	if len(ctx.Categories["identity"]) != 2 {
		t.Fatalf("expected 2 identity fields in context, got %d", len(ctx.Categories["identity"]))
	}

	w = env.doRequestWithToken(t, "GET", "/vault/fields/category/payment", nil, token)
	var fields []vault.FieldInfo
	json.NewDecoder(w.Body).Decode(&fields)
	if len(fields) != 0 {
		t.Fatalf("category read leaked %d critical fields", len(fields))
	}
}

func TestServiceToken_StandardTierBlocksSensitive(t *testing.T) {
	env := setup(t)
	seedTiers(t, env)
	token := createTokenWithTier(t, env, "identity.*", "standard")

	if w := env.doRequestWithToken(t, "GET", "/vault/fields/identity.codice_fiscale", nil, token); w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/identity.full_name", nil, token); w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestServiceToken_CriticalOptIn(t *testing.T) {
	env := setup(t)
	seedTiers(t, env)
	token := createTokenWithTier(t, env, "payment.*", "critical")

	if w := env.doRequestWithToken(t, "GET", "/vault/fields/payment.card_number", nil, token); w.Code != 200 {
		t.Fatalf("expected 200 with critical opt-in, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServiceToken_CannotOverwriteOrDeleteAboveTier(t *testing.T) {
	env := setup(t)
	seedTiers(t, env)
	token := createTokenWithAccess(t, env, "*", "write")

	w := env.doRequestWithToken(t, "PUT", "/vault/fields/payment.card_number", map[string]string{"value": "4111"}, token)
	if w.Code != 403 {
		t.Fatalf("overwrite critical: expected 403, got %d", w.Code)
	}
	w = env.doRequestWithToken(t, "DELETE", "/vault/fields/payment.card_number", nil, token)
	if w.Code != 403 {
		t.Fatalf("delete critical: expected 403, got %d", w.Code)
	}
	// New fields can still be written.
	w = env.doRequestWithToken(t, "PUT", "/vault/fields/residenza.comune", map[string]string{"value": "Napoli"}, token)
	if w.Code != 200 {
		t.Fatalf("write new field: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServiceToken_CannotChangeSensitivity(t *testing.T) {
	env := setup(t)
	seedTiers(t, env)
	token := createScopedToken(t, env, "agent", "*")

	// Lowering a tier would let the token read past its cap.
	w := env.doRequestWithToken(t, "PUT", "/vault/sensitivity/payment.card_number", map[string]string{"tier": "public"}, token)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "session_required") {
		t.Fatalf("expected 403 session_required, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateServiceToken_RequiresScope(t *testing.T) {
	env := setup(t)
	w := env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "agent"}, true)
	if w.Code != 400 {
		t.Fatalf("missing scope: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	w = env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "agent", "scope": "identity"}, true)
	if w.Code != 400 {
		t.Fatalf("malformed scope: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateServiceToken_DefaultsAndLimits(t *testing.T) {
	env := setup(t)
	w := env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "agent", "scope": "identity.*"}, true)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["max_sensitivity"] != "sensitive" {
		t.Errorf("default max_sensitivity = %q, want sensitive", resp["max_sensitivity"])
	}

	w = env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "agent", "scope": "*", "ttl": "8760h"}, true)
	if w.Code != 400 {
		t.Fatalf("1-year ttl: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	w = env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "agent", "scope": "*", "max_sensitivity": "top-secret"}, true)
	if w.Code != 400 {
		t.Fatalf("bad tier: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokeServiceToken_ByListedPrefix(t *testing.T) {
	env := setup(t)
	token := createScopedToken(t, env, "agent", "*")

	w := env.doRequest(t, "GET", "/vault/tokens/service", nil, true)
	var list []struct {
		TokenPrefix string `json:"token_prefix"`
	}
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("expected 1 token, got %d", len(list))
	}

	// The prefix exactly as list-service-tokens prints it, including "...".
	w = env.doRequest(t, "DELETE", "/vault/tokens/service/"+list[0].TokenPrefix, nil, true)
	if w.Code != 200 {
		t.Fatalf("revoke by prefix: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/fields", nil, token); w.Code != 401 {
		t.Fatalf("revoked token: expected 401, got %d", w.Code)
	}
}

func TestRevokeServiceToken_ShortPrefixRejected(t *testing.T) {
	env := setup(t)
	createScopedToken(t, env, "agent", "*")

	w := env.doRequest(t, "DELETE", "/vault/tokens/service/abc", nil, true)
	if w.Code != 404 {
		t.Fatalf("short prefix: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSessionGrant_API(t *testing.T) {
	env, sk := setupWithSecretKey(t)

	w := env.doRequest(t, "POST", "/vault/session", map[string]string{
		"password": testPassword, "secret_key": sk, "ttl": "1m",
	}, false)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	grant := resp["token"]
	if grant == "" || resp["expires_at"] == "" {
		t.Fatalf("missing token or expiry: %v", resp)
	}

	// A grant has full session rights, e.g. creating service tokens.
	w = env.doRequestWithToken(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "a", "scope": "*"}, grant)
	if w.Code != 200 {
		t.Fatalf("grant as session: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = env.doRequest(t, "POST", "/vault/session", map[string]string{
		"password": "wrong", "secret_key": sk,
	}, false)
	if w.Code != 401 {
		t.Fatalf("wrong password: expected 401, got %d", w.Code)
	}
	w = env.doRequest(t, "POST", "/vault/session", map[string]string{
		"password": testPassword, "secret_key": sk, "ttl": "24h",
	}, false)
	if w.Code != 400 {
		t.Fatalf("ttl above max: expected 400, got %d", w.Code)
	}
}

func TestSessionGrant_Locked(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	env.vault.Lock()
	w := env.doRequest(t, "POST", "/vault/session", map[string]string{
		"password": testPassword, "secret_key": sk,
	}, false)
	if w.Code != 403 {
		t.Fatalf("locked: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSessionGrant_RateLimited(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	var last int
	for i := 0; i < 11; i++ {
		last = env.doRequest(t, "POST", "/vault/session", map[string]string{
			"password": "wrong", "secret_key": sk,
		}, false).Code
	}
	if last != 429 {
		t.Fatalf("expected 429 after 10 attempts, got %d", last)
	}
}

func setupWithSecretKey(t *testing.T) (*testEnv, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".pvault")
	sk, err := vault.Init(dir, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	token, err := v.Unlock(testPassword, sk)
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{server: New(v, ":0"), vault: v, token: token}, sk
}

func TestSetField_OverwriteKeepsRaisedTier(t *testing.T) {
	env, _ := setupWithSecretKey(t)
	env.doRequest(t, "PUT", "/vault/fields/identity.codice_fiscale", map[string]string{"value": "RSSMRA85T10A562S"}, true)
	env.doRequest(t, "PUT", "/vault/sensitivity/identity.codice_fiscale", map[string]string{"tier": "critical"}, true)
	env.doRequest(t, "PUT", "/vault/fields/identity.codice_fiscale", map[string]string{"value": "CCCFBA85D03L219P"}, true)

	token := createScopedToken(t, env, "agent", "*")
	w := env.doRequestWithToken(t, "GET", "/vault/fields/identity.codice_fiscale", nil, token)
	if w.Code != 403 {
		t.Fatalf("raised tier was lost on overwrite: agent got %d %s", w.Code, w.Body.String())
	}
}

func TestSessionGrant_PasswordOnly_API(t *testing.T) {
	env, _ := setupWithSecretKey(t)
	w := env.doRequest(t, "POST", "/vault/session", map[string]string{"password": testPassword, "ttl": "15m"}, false)
	if w.Code != 200 {
		t.Fatalf("password-only grant: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSessionGrant_SuccessesDoNotCount(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	for i := 0; i < 15; i++ {
		w := env.doRequest(t, "POST", "/vault/session", map[string]string{"password": testPassword, "secret_key": sk}, false)
		if w.Code != 200 {
			t.Fatalf("grant %d: expected 200, got %d: %s", i+1, w.Code, w.Body.String())
		}
	}
}

func TestCredentialEndpoints_RequireJSON(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	body := `{"password":"` + testPassword + `","secret_key":"` + sk + `"}`
	for _, path := range []string{"/vault/session", "/vault/unlock"} {
		// A cross-site form or fetch without preflight can only send text/plain.
		req := newTestRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		env.server.handler.ServeHTTP(w, req)
		if w.Code != 415 {
			t.Errorf("%s with text/plain: expected 415, got %d", path, w.Code)
		}
	}
}

func TestHostHeader_RebindingRejected(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	body := `{"password":"` + testPassword + `","secret_key":"` + sk + `"}`
	for _, host := range []string{"evil.example", "attacker.com:7200", "127.0.0.1.nip.io"} {
		req := httptest.NewRequest("POST", "/vault/session", strings.NewReader(body))
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		env.server.handler.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Errorf("Host %q: expected 403, got %d", host, w.Code)
		}
	}
	for _, host := range []string{"127.0.0.1:7200", "localhost:7200", "[::1]:7200", "localhost"} {
		req := httptest.NewRequest("GET", "/vault/status", nil)
		req.Host = host
		w := httptest.NewRecorder()
		env.server.handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("Host %q: expected 200, got %d", host, w.Code)
		}
	}
}

func TestSessionGrant_ConcurrentFailuresBounded(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	const n = 30
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- env.doRequest(t, "POST", "/vault/session", map[string]string{"password": "wrong", "secret_key": sk}, false).Code
		}()
	}
	wg.Wait()
	close(codes)
	evaluated := 0
	for c := range codes {
		if c == 401 {
			evaluated++
		}
	}
	if evaluated > 10 {
		t.Fatalf("%d concurrent wrong passwords were evaluated, want at most 10", evaluated)
	}
	// Successes are refunded, so a correct password still works after the
	// window clears; within it, the limit holds.
	if w := env.doRequest(t, "POST", "/vault/session", map[string]string{"password": testPassword, "secret_key": sk}, false); w.Code != 429 {
		t.Fatalf("expected 429 while limited, got %d", w.Code)
	}
}

func TestSessionGrant_NonCredentialErrorsDoNotCount(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	for i := 0; i < 12; i++ {
		env.doRequest(t, "POST", "/vault/session", map[string]string{"password": testPassword, "secret_key": sk, "ttl": "24h"}, false)
	}
	if w := env.doRequest(t, "POST", "/vault/session", map[string]string{"password": testPassword, "secret_key": sk}, false); w.Code != 200 {
		t.Fatalf("invalid-ttl requests consumed the limit: %d", w.Code)
	}
}

func TestServiceToken_DefaultIsReadOnly(t *testing.T) {
	env := setup(t)
	env.doRequest(t, "PUT", "/vault/fields/residenza.comune", map[string]string{"value": "Napoli"}, true)
	var doc vault.DocumentInfo
	json.NewDecoder(env.upload(t, env.token, "category=residenza&name=r.pdf", "application/pdf", []byte("r")).Body).Decode(&doc)

	w := env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "a", "scope": "residenza.*"}, true)
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	if created["access"] != "read" {
		t.Fatalf("default access = %q, want read", created["access"])
	}
	token := created["token"]

	if w := env.doRequestWithToken(t, "GET", "/vault/fields/residenza.comune", nil, token); w.Code != 200 {
		t.Fatalf("read: %d", w.Code)
	}
	checks := []struct {
		name string
		code int
	}{
		{"new field", env.doRequestWithToken(t, "PUT", "/vault/fields/residenza.cap", map[string]string{"value": "80121"}, token).Code},
		{"overwrite", env.doRequestWithToken(t, "PUT", "/vault/fields/residenza.comune", map[string]string{"value": "Roma"}, token).Code},
		{"delete field", env.doRequestWithToken(t, "DELETE", "/vault/fields/residenza.comune", nil, token).Code},
		{"upload", env.upload(t, token, "category=residenza&name=x.pdf", "application/pdf", []byte("x")).Code},
		{"delete document", env.doRequestWithToken(t, "DELETE", "/vault/documents/"+doc.ID, nil, token).Code},
	}
	for _, c := range checks {
		if c.code != 403 {
			t.Errorf("read-only token %s: expected 403, got %d", c.name, c.code)
		}
	}
	w = env.doRequestWithToken(t, "PUT", "/vault/fields/residenza.cap", map[string]string{"value": "80121"}, token)
	if !strings.Contains(w.Body.String(), "read_only") {
		t.Errorf("expected read_only constraint, got %s", w.Body.String())
	}
}

func TestServiceToken_AppendAccess(t *testing.T) {
	env := setup(t)
	env.doRequest(t, "PUT", "/vault/fields/residenza.comune", map[string]string{"value": "Napoli"}, true)
	token := createTokenWithAccess(t, env, "residenza.*", "append")

	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/residenza.cap", map[string]string{"value": "80121"}, token); w.Code != 200 {
		t.Fatalf("append new field: %d %s", w.Code, w.Body.String())
	}
	w := env.doRequestWithToken(t, "PUT", "/vault/fields/residenza.comune", map[string]string{"value": "Roma"}, token)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "append_only") {
		t.Fatalf("append overwrite: expected 403 append_only, got %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequestWithToken(t, "DELETE", "/vault/fields/residenza.cap", nil, token); w.Code != 403 {
		t.Fatalf("append delete field: %d", w.Code)
	}
	w = env.upload(t, token, "category=residenza&name=receipt.pdf", "application/pdf", []byte("r"))
	if w.Code != 200 {
		t.Fatalf("append upload: %d %s", w.Code, w.Body.String())
	}
	var doc vault.DocumentInfo
	json.NewDecoder(w.Body).Decode(&doc)
	if w := env.doRequestWithToken(t, "DELETE", "/vault/documents/"+doc.ID, nil, token); w.Code != 403 {
		t.Fatalf("append delete document: %d", w.Code)
	}
	// The original value is untouched.
	w = env.doRequest(t, "GET", "/vault/fields/residenza.comune", nil, true)
	var f vault.FieldInfo
	json.NewDecoder(w.Body).Decode(&f)
	if f.Value != "Napoli" {
		t.Fatalf("value changed to %q", f.Value)
	}
}

func TestServiceToken_WriteAccessAndValidation(t *testing.T) {
	env := setup(t)
	env.doRequest(t, "PUT", "/vault/fields/residenza.comune", map[string]string{"value": "Napoli"}, true)
	token := createTokenWithAccess(t, env, "residenza.*", "write")
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/residenza.comune", map[string]string{"value": "Roma"}, token); w.Code != 200 {
		t.Fatalf("write overwrite: %d", w.Code)
	}
	if w := env.doRequestWithToken(t, "DELETE", "/vault/fields/residenza.comune", nil, token); w.Code != 200 {
		t.Fatalf("write delete: %d", w.Code)
	}
	w := env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "a", "scope": "*", "access": "admin"}, true)
	if w.Code != 400 {
		t.Fatalf("invalid access: expected 400, got %d", w.Code)
	}
	w = env.doRequest(t, "GET", "/vault/tokens/service", nil, true)
	var list []map[string]string
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 || list[0]["access"] != "write" {
		t.Fatalf("list should show access: %+v", list)
	}
}

func TestPersonalTier_InvisibleToAgents(t *testing.T) {
	env := setup(t)
	for id, v := range map[string]string{"spid.password": "hunter2", "spid.provider": "PosteID", "cie.pin": "12345678", "identity.full_name": "Mario Rossi"} {
		if w := env.doRequest(t, "PUT", "/vault/fields/"+id, map[string]string{"value": v}, true); w.Code != 200 {
			t.Fatalf("seed %s: %d %s", id, w.Code, w.Body.String())
		}
	}
	env.doRequest(t, "PUT", "/vault/fields/spid.data_scadenza", map[string]string{"value": "2000-01-01"}, true)
	var doc vault.DocumentInfo
	json.NewDecoder(env.upload(t, env.token, "category=spid&name=recovery.png", "image/png", []byte("codes")).Body).Decode(&doc)
	if doc.Sensitivity != vault.TierPersonal {
		t.Fatalf("spid document tier = %q", doc.Sensitivity)
	}

	// Even the broadest agent token: full scope, critical cap, write access.
	token := createTokenWithAccess(t, env, "*", "write")
	w := env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "x", "scope": "*", "max_sensitivity": "critical", "access": "write"}, true)
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	critical := created["token"]

	for _, tok := range []string{token, critical} {
		for _, id := range []string{"spid.password", "spid.provider", "cie.pin"} {
			if w := env.doRequestWithToken(t, "GET", "/vault/fields/"+id, nil, tok); w.Code != 404 {
				t.Errorf("GET %s: expected 404, got %d", id, w.Code)
			}
			if w := env.doRequestWithToken(t, "PUT", "/vault/fields/"+id, map[string]string{"value": "x"}, tok); w.Code != 403 {
				t.Errorf("PUT %s: expected 403, got %d", id, w.Code)
			}
			if w := env.doRequestWithToken(t, "DELETE", "/vault/fields/"+id, nil, tok); w.Code != 404 {
				t.Errorf("DELETE %s: expected 404, got %d", id, w.Code)
			}
		}
		// Creating new personal fields or documents is refused too.
		if w := env.doRequestWithToken(t, "PUT", "/vault/fields/spid.otp", map[string]string{"value": "123456"}, tok); w.Code != 403 {
			t.Errorf("create spid.otp: %d", w.Code)
		}
		if w := env.doRequestWithToken(t, "PUT", "/vault/fields/identity.note", map[string]string{"value": "x", "sensitivity": "personal"}, tok); w.Code != 403 {
			t.Errorf("create personal note: %d", w.Code)
		}
		if w := env.upload(t, tok, "category=spid&name=x.png", "image/png", []byte("x")); w.Code != 403 {
			t.Errorf("upload spid doc: %d", w.Code)
		}
		if w := env.doRequestWithToken(t, "GET", "/vault/documents/"+doc.ID, nil, tok); w.Code != 404 {
			t.Errorf("GET personal doc: %d", w.Code)
		}

		body := func(path string) string {
			return env.doRequestWithToken(t, "GET", path, nil, tok).Body.String()
		}
		for _, path := range []string{"/vault/fields", "/vault/context", "/vault/fields/category/spid", "/vault/fields/category/cie", "/vault/documents", "/vault/expiring?days=10"} {
			b := body(path)
			if strings.Contains(b, "spid.") || strings.Contains(b, "cie.pin") || strings.Contains(b, "hunter2") || strings.Contains(b, doc.ID) {
				t.Errorf("%s leaked personal data: %s", path, b)
			}
		}
		if !strings.Contains(body("/vault/context"), "Mario Rossi") {
			t.Error("non-personal data should still be visible")
		}
	}

	// The user's own session sees everything, including the renewal date.
	if w := env.doRequest(t, "GET", "/vault/fields/spid.password", nil, true); !strings.Contains(w.Body.String(), "hunter2") {
		t.Errorf("session read: %s", w.Body.String())
	}
	if w := env.doRequest(t, "GET", "/vault/expiring?days=10", nil, true); !strings.Contains(w.Body.String(), "spid.data_scadenza") {
		t.Errorf("session expiring should include SPID renewal: %s", w.Body.String())
	}
	// Agents can never be given personal access.
	if w := env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "x", "scope": "*", "max_sensitivity": "personal"}, true); w.Code != 400 {
		t.Errorf("personal cap: expected 400, got %d", w.Code)
	}
}

func TestPersonalTier_StatusAndLoweredFields(t *testing.T) {
	env := setup(t)
	env.doRequest(t, "PUT", "/vault/fields/residenza.comune", map[string]string{"value": "Napoli"}, true)
	env.doRequest(t, "PUT", "/vault/fields/spid.password", map[string]string{"value": "hunter2"}, true)
	env.doRequest(t, "PUT", "/vault/fields/spid.data_scadenza", map[string]string{"value": "2027-01-31"}, true)
	env.upload(t, env.token, "category=spid&name=codes.png", "image/png", []byte("x"))

	// Public status must not reveal personal fields or documents.
	w := env.doRequest(t, "GET", "/vault/status", nil, false)
	var st vault.VaultStatus
	json.NewDecoder(w.Body).Decode(&st)
	if st.FieldCount != 1 || st.Categories["spid"] != 0 || st.Documents != 0 {
		t.Fatalf("status leaks personal items: %+v", st)
	}

	// A non-secret SPID detail the user lowered can be maintained by an agent...
	if w := env.doRequest(t, "PUT", "/vault/sensitivity/spid.data_scadenza", map[string]string{"tier": "standard"}, true); w.Code != 200 {
		t.Fatalf("lower renewal date: %d %s", w.Code, w.Body.String())
	}
	token := createTokenWithAccess(t, env, "spid.*", "write")
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/spid.data_scadenza", map[string]string{"value": "2028-01-31"}, token); w.Code != 200 {
		t.Fatalf("agent update of lowered field: %d %s", w.Code, w.Body.String())
	}
	// ...but credentials under any name can't be lowered.
	env.doRequest(t, "PUT", "/vault/fields/spid.pass", map[string]string{"value": "x"}, true)
	if w := env.doRequest(t, "PUT", "/vault/sensitivity/spid.pass", map[string]string{"tier": "standard"}, true); w.Code != 400 {
		t.Fatalf("lowering spid.pass: expected 400, got %d", w.Code)
	}
}

func scopedGrant(t *testing.T, env *testEnv, sk, scope, access string) string {
	t.Helper()
	w := env.doRequest(t, "POST", "/vault/session", map[string]string{
		"password": testPassword, "secret_key": sk, "ttl": "2m", "scope": scope, "access": access,
	}, false)
	if w.Code != 200 {
		t.Fatalf("scoped grant: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["scope"] != scope || resp["access"] != access {
		t.Fatalf("grant should echo scope and access, got %v", resp)
	}
	return resp["token"]
}

// A scoped grant (what a Touch ID approval returns) reaches exactly the
// approved fields, personal ones included, and nothing session-only.
func TestScopedGrant_API(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	for id, v := range map[string]string{"spid.password": "hunter2", "identity.full_name": "Mario Rossi", "residenza.comune": "Napoli"} {
		if w := env.doRequest(t, "PUT", "/vault/fields/"+id, map[string]string{"value": v}, true); w.Code != 200 {
			t.Fatalf("seed %s: %d %s", id, w.Code, w.Body.String())
		}
	}

	read := scopedGrant(t, env, sk, "spid.password,identity.*", "read")
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/spid.password", nil, read); !strings.Contains(w.Body.String(), "hunter2") {
		t.Errorf("approved personal field: %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/identity.full_name", nil, read); w.Code != 200 {
		t.Errorf("approved category: %d", w.Code)
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/residenza.comune", nil, read); w.Code != 403 {
		t.Errorf("field outside scope: expected 403, got %d", w.Code)
	}
	if b := env.doRequestWithToken(t, "GET", "/vault/context", nil, read).Body.String(); strings.Contains(b, "Napoli") {
		t.Errorf("context leaked a field outside scope: %s", b)
	}
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/identity.full_name", map[string]string{"value": "x"}, read); w.Code != 403 {
		t.Errorf("write with read grant: expected 403, got %d", w.Code)
	}
	for _, req := range []struct{ method, path string }{
		{"GET", "/vault/audit"}, {"POST", "/vault/lock"}, {"GET", "/vault/tokens/service"},
	} {
		if w := env.doRequestWithToken(t, req.method, req.path, nil, read); w.Code != 403 {
			t.Errorf("%s %s with scoped grant: expected 403, got %d", req.method, req.path, w.Code)
		}
	}

	write := scopedGrant(t, env, sk, "residenza.comune", "write")
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/residenza.comune", map[string]string{"value": "Roma"}, write); w.Code != 200 {
		t.Errorf("approved write: %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequestWithToken(t, "DELETE", "/vault/fields/residenza.comune", nil, write); w.Code != 200 {
		t.Errorf("approved delete: %d %s", w.Code, w.Body.String())
	}

	add := scopedGrant(t, env, sk, "identity.*", "append")
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/identity.full_name", map[string]string{"value": "x"}, add); w.Code != 403 {
		t.Errorf("overwrite with append grant: expected 403, got %d", w.Code)
	}
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/identity.nickname", map[string]string{"value": "Mario"}, add); w.Code != 200 {
		t.Errorf("new field with append grant: %d %s", w.Code, w.Body.String())
	}

	for _, body := range []map[string]string{
		{"password": testPassword, "scope": "identity"},
		{"password": testPassword, "scope": "identity.*", "ttl": "11m"},
		{"password": testPassword, "access": "write"},
	} {
		if w := env.doRequest(t, "POST", "/vault/session", body, false); w.Code != 400 {
			t.Errorf("%v: expected 400, got %d", body, w.Code)
		}
	}
}

// Tokens and scoped grants may raise a tier on write but never lower it.
func TestFieldWrite_TokensCannotLowerTier(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	seedTiers(t, env)
	svc := createTokenWithAccess(t, env, "*", "write")
	grant := scopedGrant(t, env, sk, "identity.*", "write")
	for _, tok := range []string{svc, grant} {
		w := env.doRequestWithToken(t, "PUT", "/vault/fields/identity.codice_fiscale", map[string]string{"value": "CCCFBA85D03L219P", "sensitivity": "public"}, tok)
		if w.Code != 403 {
			t.Errorf("lowering existing tier: expected 403, got %d %s", w.Code, w.Body.String())
		}
		w = env.doRequestWithToken(t, "PUT", "/vault/fields/identity.other_codice_fiscale", map[string]string{"value": "CCCFBA85D03L219P", "sensitivity": "public"}, tok)
		if w.Code != 403 {
			t.Errorf("new field below schema default: expected 403, got %d %s", w.Code, w.Body.String())
		}
	}
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/identity.full_name", map[string]string{"value": "Mario", "sensitivity": "sensitive"}, svc); w.Code != 200 {
		t.Errorf("raising tier: expected 200, got %d %s", w.Code, w.Body.String())
	}
	f, _ := env.vault.Get("identity.codice_fiscale")
	if f.Sensitivity != "sensitive" {
		t.Errorf("tier changed to %s", f.Sensitivity)
	}
	if w := env.doRequest(t, "PUT", "/vault/fields/identity.full_name", map[string]string{"value": "Mario", "sensitivity": "public"}, true); w.Code != 200 {
		t.Errorf("session lowering: expected 200, got %d", w.Code)
	}
}

// The portal-login flow: the user stores their login method and SPID provider,
// lowers the provider from personal, and an agent token scoped to just those
// two fields can read them in their stored spelling.
func TestPortalLoginPreference_AgentFlow(t *testing.T) {
	env := setup(t)
	token := createScopedToken(t, env, "claude", "preferences.accesso_pa,spid.provider")

	for id, v := range map[string]string{"preferences.accesso_pa": "SPID", "spid.provider": "poste id"} {
		if w := env.doRequest(t, "PUT", "/vault/fields/"+id, map[string]string{"value": v}, true); w.Code != 200 {
			t.Fatalf("set %s: %d %s", id, w.Code, w.Body.String())
		}
	}
	if w := env.doRequest(t, "PUT", "/vault/fields/spid.provider", map[string]string{"value": "Google"}, true); w.Code != 400 {
		t.Errorf("unknown provider: expected 400, got %d", w.Code)
	}

	get := func(id string) (int, vault.FieldInfo) {
		w := env.doRequestWithToken(t, "GET", "/vault/fields/"+id, nil, token)
		var f vault.FieldInfo
		json.NewDecoder(w.Body).Decode(&f)
		return w.Code, f
	}
	if code, f := get("preferences.accesso_pa"); code != 200 || f.Value != "spid" {
		t.Errorf("accesso_pa: %d %q, want 200 spid", code, f.Value)
	}
	// Still personal: hidden from the agent until the user lowers it.
	if code, _ := get("spid.provider"); code != 404 {
		t.Errorf("personal spid.provider: expected 404, got %d", code)
	}
	if w := env.doRequest(t, "PUT", "/vault/sensitivity/spid.provider", map[string]string{"tier": "standard"}, true); w.Code != 200 {
		t.Fatalf("lower spid.provider: %d %s", w.Code, w.Body.String())
	}
	if code, f := get("spid.provider"); code != 200 || f.Value != "PosteID" {
		t.Errorf("spid.provider: %d %q, want 200 PosteID", code, f.Value)
	}
	// Overwriting the provider keeps the lowered tier.
	env.doRequest(t, "PUT", "/vault/fields/spid.provider", map[string]string{"value": "aruba"}, true)
	if code, f := get("spid.provider"); code != 200 || f.Value != "Aruba" || f.Sensitivity != "standard" {
		t.Errorf("after overwrite: %d %q %s", code, f.Value, f.Sensitivity)
	}
	// The rest of the spid category stays out of reach.
	env.doRequest(t, "PUT", "/vault/fields/spid.password", map[string]string{"value": "hunter2"}, true)
	if w := env.doRequest(t, "PUT", "/vault/sensitivity/spid.password", map[string]string{"tier": "standard"}, true); w.Code != 400 {
		t.Errorf("lowering spid.password: expected 400, got %d", w.Code)
	}
	if code, _ := get("spid.password"); code != 403 && code != 404 {
		t.Errorf("spid.password readable by the agent: %d", code)
	}
}

// createWriteToken makes a write token with the given scope that can see
// up to critical.
func createWriteToken(t *testing.T, env *testEnv, scope string) string {
	t.Helper()
	return createToken(t, env, map[string]string{"scope": scope, "access": "write", "max_sensitivity": "critical"})
}

// Deleting a field and writing it again would give it the default tier, so
// agents may not delete fields stored above it.
func TestDeleteCannotLowerFieldTier(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	env.doRequest(t, "PUT", "/vault/fields/identity.note", map[string]string{"value": "secret-note", "sensitivity": "critical"}, true)
	env.doRequest(t, "PUT", "/vault/fields/identity.plain", map[string]string{"value": "x"}, true)
	svc := createWriteToken(t, env, "identity.*")

	if w := env.doRequestWithToken(t, "DELETE", "/vault/fields/identity.note", nil, svc); w.Code != 403 || !strings.Contains(w.Body.String(), "session_required") {
		t.Errorf("token deleting a critical field: %d %s, want 403 session_required", w.Code, w.Body.String())
	}
	if w := env.doRequestWithToken(t, "DELETE", "/vault/fields/identity.plain", nil, svc); w.Code != 200 {
		t.Errorf("token deleting a field at its default tier: %d %s", w.Code, w.Body.String())
	}

	// A write grant the user approved may still not lower a personal field.
	env.doRequest(t, "PUT", "/vault/fields/residenza.note", map[string]string{"value": "very-private", "sensitivity": "personal"}, true)
	grant := scopedGrant(t, env, sk, "residenza.note", "write")
	if w := env.doRequestWithToken(t, "DELETE", "/vault/fields/residenza.note", nil, grant); w.Code != 403 {
		t.Errorf("scoped grant deleting a personal field: %d %s, want 403", w.Code, w.Body.String())
	}
	if w := env.doRequest(t, "DELETE", "/vault/fields/identity.note", nil, true); w.Code != 200 {
		t.Errorf("session delete: %d %s", w.Code, w.Body.String())
	}
}

func TestDeleteCannotLowerDocumentTier(t *testing.T) {
	env := setup(t)
	if w := env.upload(t, env.token, "category=passaporto&name=scan.pdf&sensitivity=critical", "application/pdf", []byte("PDFDATA")); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	svc := createWriteToken(t, env, "passaporto.*")
	var docs []struct{ ID string }
	json.NewDecoder(env.doRequestWithToken(t, "GET", "/vault/documents", nil, svc).Body).Decode(&docs)
	if len(docs) != 1 {
		t.Fatal(docs)
	}
	if w := env.doRequestWithToken(t, "DELETE", "/vault/documents/"+docs[0].ID, nil, svc); w.Code != 403 || !strings.Contains(w.Body.String(), "session_required") {
		t.Errorf("token deleting a critical document: %d %s, want 403 session_required", w.Code, w.Body.String())
	}
	if w := env.doRequest(t, "DELETE", "/vault/documents/"+docs[0].ID, nil, true); w.Code != 200 {
		t.Errorf("session delete: %d %s", w.Code, w.Body.String())
	}
}

// A token can't tell a hidden personal field from a missing one by deleting.
func TestDeleteDoesNotRevealPersonalFields(t *testing.T) {
	env := setup(t)
	env.doRequest(t, "PUT", "/vault/fields/identity.diary", map[string]string{"value": "x", "sensitivity": "personal"}, true)
	for _, access := range []string{"read", "write"} {
		tok := createTokenWithAccess(t, env, "identity.*", access)
		a := env.doRequestWithToken(t, "DELETE", "/vault/fields/identity.diary", nil, tok)
		b := env.doRequestWithToken(t, "DELETE", "/vault/fields/identity.nothing_here", nil, tok)
		if a.Code != b.Code {
			t.Errorf("%s token: personal field %d, missing field %d", access, a.Code, b.Code)
		}
	}
}

// Refunding an attempt that already expired must not erase a newer failed
// one, or slow requests could buy extra password guesses.
func TestRateLimiterRefundsOnlyItsOwnAttempt(t *testing.T) {
	rl := newRateLimiter(2, 50*time.Millisecond)
	slow, ok := rl.allow()
	if !ok {
		t.Fatal("first attempt refused")
	}
	time.Sleep(80 * time.Millisecond) // the slow request's attempt expires
	for i := 0; i < 2; i++ {
		if _, ok := rl.allow(); !ok {
			t.Fatalf("attempt %d refused", i+2)
		}
	}
	rl.refund(slow)
	if _, ok := rl.allow(); ok {
		t.Error("refunding an expired attempt freed a slot held by a newer failed attempt")
	}
}

// A read-only token gets the same answer for a hidden personal field as for
// a missing one, on PUT as on DELETE.
func TestPutDoesNotRevealPersonalFields(t *testing.T) {
	env := setup(t)
	env.doRequest(t, "PUT", "/vault/fields/identity.diary", map[string]string{"value": "x", "sensitivity": "personal"}, true)
	tok := createTokenWithAccess(t, env, "identity.*", "read")
	a := env.doRequestWithToken(t, "PUT", "/vault/fields/identity.diary", map[string]string{"value": "y"}, tok)
	b := env.doRequestWithToken(t, "PUT", "/vault/fields/identity.nothing_here", map[string]string{"value": "y"}, tok)
	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		t.Errorf("existing personal field: %d %s\nmissing field: %d %s", a.Code, a.Body.String(), b.Code, b.Body.String())
	}
}

// A token's write checked against a field that changes meanwhile (here the
// user creating it as personal) is refused, never applied to the new field.
func TestTokenWriteRacingUserWrite(t *testing.T) {
	env := setup(t)
	tok := createTokenWithAccess(t, env, "identity.*", "append")
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("identity.secret%d", i)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			env.doRequest(t, "PUT", "/vault/fields/"+id, map[string]string{"value": "user", "sensitivity": "personal"}, true)
		}()
		go func() {
			defer wg.Done()
			env.doRequestWithToken(t, "PUT", "/vault/fields/"+id, map[string]string{"value": "agent"}, tok)
		}()
		wg.Wait()
		if f, _ := env.vault.Get(id); f != nil && f.Sensitivity == vault.TierPersonal && f.Value == "agent" {
			t.Fatalf("%s: the token's value landed in the user's personal field", id)
		}
	}
}

func TestSessionChangesToMissingFieldsAre404(t *testing.T) {
	env := setup(t)
	if w := env.doRequest(t, "PUT", "/vault/sensitivity/notes.pin", map[string]string{"tier": "critical"}, true); w.Code != 404 {
		t.Errorf("set-sensitivity on a missing field: %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequest(t, "DELETE", "/vault/fields/notes.pin", nil, true); w.Code != 404 {
		t.Errorf("delete of a missing field: %d %s", w.Code, w.Body.String())
	}
}

// An expired token no longer shows up as active.
func TestListServiceTokensOmitsExpired(t *testing.T) {
	env := setup(t)
	if _, err := env.vault.CreateServiceToken("short", "notes.*", 50*time.Millisecond, "", ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	w := env.doRequest(t, "GET", "/vault/tokens/service", nil, true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "short") {
		t.Errorf("list: %d %s", w.Code, w.Body.String())
	}
}

// A SPID document stored at its own default tier (personal) is not "above
// the default", so a scoped grant the user approved may delete it, as it may
// delete spid fields.
func TestScopedGrantDeletesSPIDDocumentAtDefaultTier(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	if w := env.upload(t, env.token, "category=spid_poste&name=codes.pdf", "application/pdf", []byte("PDFDATA")); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var docs []struct{ ID, Sensitivity string }
	json.NewDecoder(env.doRequest(t, "GET", "/vault/documents", nil, true).Body).Decode(&docs)
	if len(docs) != 1 || docs[0].Sensitivity != vault.TierPersonal {
		t.Fatalf("documents = %+v", docs)
	}
	grant := scopedGrant(t, env, sk, "spid_poste.*", "write")
	if w := env.doRequestWithToken(t, "DELETE", "/vault/documents/"+docs[0].ID, nil, grant); w.Code != 200 {
		t.Errorf("grant deleting a SPID document: %d %s", w.Code, w.Body.String())
	}
}

// The audit log uses the API's snake_case field names, like everything else.
func TestAuditLogFieldNames(t *testing.T) {
	env := setup(t)
	env.doRequest(t, "PUT", "/vault/fields/identity.full_name", map[string]string{"value": "Mario Rossi"}, true)
	w := env.doRequest(t, "GET", "/vault/audit?limit=5", nil, true)
	var entries []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil || len(entries) == 0 {
		t.Fatalf("audit: %v %s", err, w.Body.String())
	}
	for _, key := range []string{"id", "consumer", "scope", "action", "created_at"} {
		if _, ok := entries[0][key]; !ok {
			t.Errorf("audit entry has no %q: %v", key, entries[0])
		}
	}
}

// Only wrong credentials count toward the unlock limit, so junk requests from
// another local process can't lock the user out.
func TestUnlock_JunkRequestsDontUseUpAttempts(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	for i := 0; i < 10; i++ {
		env.doRequest(t, "POST", "/vault/unlock", map[string]string{}, false)
	}
	env.vault.Lock()
	w := env.doRequest(t, "POST", "/vault/unlock", map[string]string{"password": testPassword, "secret_key": sk}, false)
	if w.Code != 200 {
		t.Fatalf("unlock after junk requests: %d %s", w.Code, w.Body.String())
	}
}
