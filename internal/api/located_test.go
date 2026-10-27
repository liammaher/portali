package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func putField(t *testing.T, env *testEnv, id, value string) {
	t.Helper()
	if w := env.doRequest(t, "PUT", "/vault/fields/"+id, map[string]string{"value": value}, true); w.Code != 200 {
		t.Fatalf("set %s: %d %s", id, w.Code, w.Body.String())
	}
}

func seedAddress(t *testing.T, env *testEnv, street, civico string) {
	t.Helper()
	putField(t, env, "residenza.indirizzo", street)
	if civico != "" {
		putField(t, env, "residenza.civico", civico)
	}
	putField(t, env, "residenza.comune", "Napoli")
}

func decodeLocated(t *testing.T, body []byte) vault.Located {
	t.Helper()
	var loc vault.Located
	if err := json.Unmarshal(body, &loc); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return loc
}

func TestLocated_GetComputedField(t *testing.T) {
	env := setup(t)
	seedAddress(t, env, "Via Alabardieri", "10")

	w := env.doRequest(t, "GET", "/vault/fields/residenza.municipalita", nil, true)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var f vault.FieldInfo
	json.NewDecoder(w.Body).Decode(&f)
	if f.Value != "1" || !f.Derived {
		t.Errorf("%+v", f)
	}
	// Stored data only in the listing.
	w = env.doRequest(t, "GET", "/vault/fields", nil, true)
	if strings.Contains(w.Body.String(), "municipalita") {
		t.Errorf("listing has a computed field: %s", w.Body.String())
	}
}

func TestLocated_NotResolvedIs404WithTheReason(t *testing.T) {
	env := setup(t)
	seedAddress(t, env, "Via Toledo", "")
	w := env.doRequest(t, "GET", "/vault/fields/residenza.municipalita", nil, true)
	if w.Code != 404 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var resp struct{ Error, Constraint string }
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Constraint != "not_resolved" || !strings.Contains(resp.Error, "Montecalvario") || strings.Contains(resp.Error, "Toledo") {
		t.Errorf("%+v", resp)
	}
}

func TestLocated_Route(t *testing.T) {
	env := setup(t)
	seedAddress(t, env, "Via Toledo", "")

	w := env.doRequest(t, "GET", "/vault/located/residenza", nil, true)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	loc := decodeLocated(t, w.Body.Bytes())
	if len(loc.Fields) != 0 || loc.Reason == "" || len(loc.Choices) != 3 {
		t.Errorf("%+v", loc)
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "toledo") {
		t.Errorf("the street is in the answer: %s", w.Body.String())
	}

	putField(t, env, "residenza.civico", "258")
	loc = decodeLocated(t, env.doRequest(t, "GET", "/vault/located/residenza", nil, true).Body.Bytes())
	if loc.Basis != "civico" || loc.Fields["residenza.municipalita"].Value != "1" || loc.Fields["residenza.distretto_asl"].Value != "24/73" {
		t.Errorf("%+v", loc)
	}

	putField(t, env, "residenza.municipalita", "2")
	loc = decodeLocated(t, env.doRequest(t, "GET", "/vault/located/residenza", nil, true).Body.Bytes())
	if f := loc.Fields["residenza.municipalita"]; f.Value != "2" || f.Source != "stored" || len(loc.Notes) != 1 {
		t.Errorf("%+v", loc)
	}

	for target, code := range map[string]int{"identity": 400, "residenza.indirizzo": 400, "Residenza": 400, "leo:domicilio": 200, "sede_legale": 200} {
		if w := env.doRequest(t, "GET", "/vault/located/"+target, nil, true); w.Code != code {
			t.Errorf("%s: %d %s", target, w.Code, w.Body.String())
		}
	}
	// No credentials, no answer.
	if w := env.doRequest(t, "GET", "/vault/located/residenza", nil, false); w.Code != 401 {
		t.Errorf("unauthenticated: %d", w.Code)
	}
}

func TestLocated_InvalidOverrideRefused(t *testing.T) {
	env := setup(t)
	for id, value := range map[string]string{
		"residenza.municipalita":  "12",
		"residenza.distretto_asl": "99",
		"residenza.quartiere":     "Centro",
	} {
		w := env.doRequest(t, "PUT", "/vault/fields/"+id, map[string]string{"value": value}, true)
		if w.Code != 400 {
			t.Errorf("%s=%q: %d %s", id, value, w.Code, w.Body.String())
		}
	}
	putField(t, env, "residenza.municipalita", "VII")
	var f vault.FieldInfo
	json.NewDecoder(env.doRequest(t, "GET", "/vault/fields/residenza.municipalita", nil, true).Body).Decode(&f)
	if f.Value != "7" || f.Derived {
		t.Errorf("%+v", f)
	}
}

func TestLocated_TokenScopes(t *testing.T) {
	env := setup(t)
	seedAddress(t, env, "Via Alabardieri", "10")

	// Only the municipalità: it is read without the street.
	narrow := createScopedToken(t, env, "narrow", "residenza.municipalita")
	w := env.doRequestWithToken(t, "GET", "/vault/fields/residenza.municipalita", nil, narrow)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"value":"1"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/residenza.indirizzo", nil, narrow); w.Code != 403 {
		t.Errorf("street: %d", w.Code)
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/residenza.distretto_asl", nil, narrow); w.Code != 403 {
		t.Errorf("distretto: %d", w.Code)
	}
	// The route needs all three computed fields in scope.
	if w := env.doRequestWithToken(t, "GET", "/vault/located/residenza", nil, narrow); w.Code != 403 {
		t.Errorf("route with one field in scope: %d", w.Code)
	}
	w = env.doRequestWithToken(t, "GET", "/vault/fields/category/residenza", nil, narrow)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Alabardieri") || !strings.Contains(w.Body.String(), "municipalita") {
		t.Errorf("category read: %d %s", w.Code, w.Body.String())
	}
	w = env.doRequestWithToken(t, "GET", "/vault/context", nil, narrow)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Alabardieri") || !strings.Contains(w.Body.String(), `"value":"1"`) {
		t.Errorf("context: %d %s", w.Code, w.Body.String())
	}

	// The three together, with the street out of scope.
	three := createScopedToken(t, env, "three", "residenza.quartiere,residenza.municipalita,residenza.distretto_asl")
	w = env.doRequestWithToken(t, "GET", "/vault/located/residenza", nil, three)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	loc := decodeLocated(t, w.Body.Bytes())
	if len(loc.Fields) != 3 || strings.Contains(w.Body.String(), "Alabardieri") {
		t.Errorf("%s", w.Body.String())
	}

	// A token can't write a computed field without write access.
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/residenza.municipalita", map[string]string{"value": "3"}, narrow); w.Code != http.StatusForbidden {
		t.Errorf("read-only token wrote: %d", w.Code)
	}
}

func TestLocated_PersonalAddressStaysHidden(t *testing.T) {
	env := setup(t)
	seedAddress(t, env, "Via Alabardieri", "")
	if w := env.doRequest(t, "PUT", "/vault/sensitivity/residenza.indirizzo", map[string]string{"tier": "personal"}, true); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	tok := createScopedToken(t, env, "agent", "residenza.*")
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/residenza.municipalita", nil, tok); w.Code != 404 {
		t.Errorf("a token read %d %s from a personal street", w.Code, w.Body.String())
	}
	if w := env.doRequest(t, "GET", "/vault/fields/residenza.municipalita", nil, true); w.Code != 200 {
		t.Errorf("the session: %d", w.Code)
	}
}

func TestLocated_ContextStoredOnlyForAnExport(t *testing.T) {
	env := setup(t)
	seedAddress(t, env, "Via Alabardieri", "")
	w := env.doRequest(t, "GET", "/vault/context", nil, true)
	if !strings.Contains(w.Body.String(), `"derived":true`) {
		t.Errorf("the context has no computed field: %s", w.Body.String())
	}
	w = env.doRequest(t, "GET", "/vault/context?stored=1", nil, true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "derived") || strings.Contains(w.Body.String(), "municipalita") || !strings.Contains(w.Body.String(), "Alabardieri") {
		t.Errorf("stored context: %d %s", w.Code, w.Body.String())
	}
	// The same scope and tier limits.
	tok := createScopedToken(t, env, "agent", "identity.*")
	w = env.doRequestWithToken(t, "GET", "/vault/context?stored=1", nil, tok)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Alabardieri") {
		t.Errorf("token: %d %s", w.Code, w.Body.String())
	}
}

func TestLocated_NotesAreNotLeakedToATokenThatCannotSeeTheField(t *testing.T) {
	env := setup(t)
	seedAddress(t, env, "Via Alabardieri", "")
	putField(t, env, "residenza.municipalita", "2")
	if w := env.doRequest(t, "PUT", "/vault/sensitivity/residenza.municipalita", map[string]string{"tier": "personal"}, true); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	tok := createScopedToken(t, env, "agent", "residenza.*")
	w := env.doRequestWithToken(t, "GET", "/vault/located/residenza", nil, tok)
	if w.Code != 200 || strings.Contains(w.Body.String(), "differs") || strings.Contains(w.Body.String(), `"value":"2"`) {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
}
