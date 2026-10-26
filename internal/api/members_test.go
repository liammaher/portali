package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func seedHousehold(t *testing.T, env *testEnv) {
	t.Helper()
	for _, kv := range [][2]string{
		{"identity.first_name", "Liam"},
		{"giulia:famiglia.relazione", "moglie"},
		{"giulia:identity.first_name", "Giulia"},
		{"leo:famiglia.relazione", "figlio"},
		{"leo:famiglia.genitori", "io,giulia"},
		{"leo:identity.first_name", "Leo"},
		{"leo:identity.date_of_birth", "2019-05-01"},
		{"leo:cie.numero", "CA12345AB"},
		{"leo:spid.password", "hunter2-secret"},
	} {
		if w := env.doRequest(t, "PUT", "/vault/fields/"+kv[0], map[string]string{"value": kv[1]}, true); w.Code != 200 {
			t.Fatalf("seed %s: %d %s", kv[0], w.Code, w.Body.String())
		}
	}
}

// Owner scopes never reach members; member scopes reach only that member.
func TestMembers_TokenScopes(t *testing.T) {
	env := setup(t)
	seedHousehold(t, env)

	owner := createTokenWithAccess(t, env, "identity.*,cie.*", "read")
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/leo:identity.first_name", nil, owner); w.Code != 403 {
		t.Fatalf("owner token read leo:identity.first_name: %d %s", w.Code, w.Body.String())
	}
	w := env.doRequestWithToken(t, "GET", "/vault/fields", nil, owner)
	if strings.Contains(w.Body.String(), "leo:") || strings.Contains(w.Body.String(), "giulia:") {
		t.Fatalf("owner token listed members: %s", w.Body.String())
	}
	w = env.doRequestWithToken(t, "GET", "/vault/members", nil, owner)
	var h vault.Household
	json.NewDecoder(w.Body).Decode(&h)
	if w.Code != 200 || len(h.Members) != 0 {
		t.Fatalf("owner token household: %d %+v", w.Code, h)
	}

	leoCIE := createTokenWithAccess(t, env, "leo:cie.*", "read")
	w = env.doRequestWithToken(t, "GET", "/vault/fields/leo:cie.numero", nil, leoCIE)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "CA12345AB") {
		t.Fatalf("leo:cie.* read leo:cie.numero: %d %s", w.Code, w.Body.String())
	}
	for _, id := range []string{"cie.numero", "leo:identity.first_name", "giulia:famiglia.relazione"} {
		if w := env.doRequestWithToken(t, "GET", "/vault/fields/"+id, nil, leoCIE); w.Code == 200 {
			t.Errorf("leo:cie.* token read %s", id)
		}
	}

	// A member's SPID credentials are personal: invisible to any token.
	all := createTokenWithTier(t, env, "*", "critical")
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/leo:spid.password", nil, all); w.Code != 404 {
		t.Fatalf("* token read leo:spid.password: %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/context", nil, all); strings.Contains(w.Body.String(), "hunter2") {
		t.Fatal("* token context included leo:spid.password")
	}
	if w := env.doRequest(t, "PUT", "/vault/fields/giulia:spid.password", map[string]string{"value": "x", "sensitivity": "standard"}, true); w.Code != 400 {
		t.Fatalf("session stored giulia:spid.password below personal: %d", w.Code)
	}
	writer := createToken(t, env, map[string]string{"scope": "leo:*", "access": "write"})
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/leo:spid.username", map[string]string{"value": "x"}, writer); w.Code != 403 {
		t.Fatalf("token wrote leo:spid.username: %d %s", w.Code, w.Body.String())
	}
	w = env.upload(t, writer, "category=leo:spid", "text/plain", []byte("codes"))
	if w.Code != 403 {
		t.Fatalf("token uploaded a leo:spid document: %d %s", w.Code, w.Body.String())
	}

	// Owner-only categories refuse a prefix, as a 400 not a 500.
	if w := env.doRequest(t, "PUT", "/vault/fields/leo:abitazione.titolo", map[string]string{"value": "proprietario"}, true); w.Code != 400 {
		t.Fatalf("leo:abitazione.titolo: %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequest(t, "PUT", "/vault/fields/leo:famiglia.genitori", map[string]string{"value": "io,marco"}, true); w.Code != 400 {
		t.Fatalf("genitori naming a missing member: %d %s", w.Code, w.Body.String())
	}
}

func TestMembers_Household(t *testing.T) {
	env := setup(t)
	seedHousehold(t, env)

	w := env.doRequest(t, "GET", "/vault/members", nil, true)
	var h vault.Household
	json.NewDecoder(w.Body).Decode(&h)
	if w.Code != 200 || len(h.Members) != 2 || h.Members[1].Slug != "leo" || h.Members[1].Minor == nil || !*h.Members[1].Minor {
		t.Fatalf("session household: %d %+v", w.Code, h)
	}

	// A famiglia-only token learns relazioni but no age, name or counts of
	// rows it can't read.
	famiglia := createTokenWithAccess(t, env, "*:famiglia.*", "read")
	w = env.doRequestWithToken(t, "GET", "/vault/members", nil, famiglia)
	h = vault.Household{}
	json.NewDecoder(w.Body).Decode(&h)
	leo := h.Members[1]
	if leo.Age != nil || leo.FirstName != "" || leo.Fields != 2 || leo.Documents != 0 || leo.RelazioneLabel != "figlio/a" {
		t.Fatalf("famiglia token saw %+v", leo)
	}
}

// The public status never reveals that members exist.
func TestMembers_StatusHidesMembers(t *testing.T) {
	env := setup(t)
	seedHousehold(t, env)
	w := env.doRequest(t, "GET", "/vault/status", nil, false)
	var status vault.VaultStatus
	json.NewDecoder(w.Body).Decode(&status)
	if strings.Contains(w.Body.String(), "leo") || strings.Contains(w.Body.String(), "giulia") || status.FieldCount != 1 {
		t.Fatalf("status = %s", w.Body.String())
	}
}

func TestMembers_RemoveAndMigrate(t *testing.T) {
	env := setup(t)
	seedHousehold(t, env)
	leoToken := createTokenWithAccess(t, env, "leo:*", "read")

	// Only the user's session may remove or migrate.
	for _, req := range [][2]string{{"DELETE", "/vault/members/leo"}, {"POST", "/vault/members/migrate"}} {
		if w := env.doRequestWithToken(t, req[0], req[1], nil, leoToken); w.Code != 403 {
			t.Fatalf("%s %s with a token: %d", req[0], req[1], w.Code)
		}
	}

	w := env.doRequest(t, "DELETE", "/vault/members/giulia", nil, true)
	var conflict struct {
		Constraint string   `json:"constraint"`
		Refs       []string `json:"refs"`
	}
	json.NewDecoder(w.Body).Decode(&conflict)
	if w.Code != 409 || conflict.Constraint != "member_in_use" || conflict.Refs[0] != "leo:famiglia.genitori" {
		t.Fatalf("remove giulia while named: %d %+v", w.Code, conflict)
	}
	w = env.doRequest(t, "DELETE", "/vault/members/leo", nil, true)
	var out vault.RemovedMember
	json.NewDecoder(w.Body).Decode(&out)
	if w.Code != 200 || out.Fields != 6 || len(out.RevokedTokens) != 1 {
		t.Fatalf("remove leo: %d %+v", w.Code, out)
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/members", nil, leoToken); w.Code != 401 {
		t.Fatalf("leo:* token after removal: %d", w.Code)
	}
	if w := env.doRequest(t, "DELETE", "/vault/members/leo", nil, true); w.Code != 404 {
		t.Fatalf("remove leo again: %d", w.Code)
	}
	if w := env.doRequest(t, "DELETE", "/vault/members/Bad_Slug", nil, true); w.Code != 400 {
		t.Fatalf("remove a bad slug: %d", w.Code)
	}

	env.doRequest(t, "PUT", "/vault/fields/nucleo_familiare.membro1_nome", map[string]string{"value": "Marco"}, true)
	w = env.doRequest(t, "POST", "/vault/members/migrate?dry_run=1", nil, true)
	var report vault.MigrationReport
	json.NewDecoder(w.Body).Decode(&report)
	if w.Code != 200 || !report.DryRun || report.Members[0].Slug != "marco" {
		t.Fatalf("migrate dry run: %d %+v", w.Code, report)
	}
	if w := env.doRequest(t, "POST", "/vault/members/migrate", nil, true); w.Code != 200 {
		t.Fatalf("migrate: %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequest(t, "GET", "/vault/fields/marco:identity.first_name", nil, true); w.Code != 200 {
		t.Fatalf("migrated field: %d", w.Code)
	}
}
