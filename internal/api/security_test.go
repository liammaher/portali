package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func createTokenWithTier(t *testing.T, env *testEnv, scope, maxSensitivity string) string {
	t.Helper()
	w := env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{
		"consumer":        "agent",
		"scope":           scope,
		"ttl":             "1h",
		"max_sensitivity": maxSensitivity,
	}, true)
	if w.Code != 200 {
		t.Fatalf("create token: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	return resp.Token
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
	token := createScopedToken(t, env, "agent", "*")

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
