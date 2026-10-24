package api

import (
	"encoding/json"
	"strings"
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
