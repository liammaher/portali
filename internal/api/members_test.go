package api

import (
	"encoding/json"
	"net/http"
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

// A token scoped to display names alone can ask whom a form is for: it sees
// each person's display name and the owner's, and nothing else of theirs.
func TestMembers_DisplayNames(t *testing.T) {
	env := setup(t)
	seedHousehold(t, env)
	for _, kv := range [][2]string{{"identity.display_name", "Liam"}, {"giulia:identity.display_name", "Giulia"}} {
		if w := env.doRequest(t, "PUT", "/vault/fields/"+kv[0], map[string]string{"value": kv[1]}, true); w.Code != 200 {
			t.Fatalf("set %s: %d %s", kv[0], w.Code, w.Body.String())
		}
	}
	if w := env.doRequest(t, "GET", "/vault/fields/giulia:identity.display_name", nil, true); !strings.Contains(w.Body.String(), `"sensitivity":"public"`) {
		t.Fatalf("display name tier: %s", w.Body.String())
	}

	names := createTokenWithAccess(t, env, "*:identity.display_name", "read")
	w := env.doRequestWithToken(t, "GET", "/vault/members", nil, names)
	var h vault.Household
	json.NewDecoder(w.Body).Decode(&h)
	if w.Code != 200 || h.Owner == nil || h.Owner.DisplayName != "Liam" || len(h.Members) != 1 {
		t.Fatalf("display-name token household: %d %s", w.Code, w.Body.String())
	}
	giulia := h.Members[0]
	if giulia.Slug != "giulia" || giulia.DisplayName != "Giulia" || giulia.FirstName != "" || giulia.Famiglia != nil || giulia.Fields != 1 {
		t.Fatalf("display-name token saw %+v", giulia)
	}
	if w := env.doRequestWithToken(t, "GET", "/vault/fields/giulia:identity.first_name", nil, names); w.Code != 403 {
		t.Fatalf("display-name token read a first name: %d", w.Code)
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
	var out vault.RemovedPerson
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

// A grant the user approved for everything about one member, with write
// access, may remove that member (its Touch ID prompt says exactly that);
// nothing narrower, and no service token, may.
func TestMembers_RemoveWithApprovedGrant(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	seedHousehold(t, env)
	env.doRequest(t, "PUT", "/vault/fields/marco:famiglia.relazione", map[string]string{"value": "fratello"}, true)

	for _, c := range []struct{ scope, access string }{
		{"leo:cie.*", "write"}, // not everything about leo
		{"leo:*", "read"},      // not write
		{"marco:*", "write"},   // another member
	} {
		g := scopedGrant(t, env, sk, c.scope, c.access)
		if w := env.doRequestWithToken(t, "DELETE", "/vault/members/leo", nil, g); w.Code != 403 {
			t.Fatalf("%s/%s removed leo: %d", c.scope, c.access, w.Code)
		}
	}
	token := createToken(t, env, map[string]string{"scope": "leo:*", "access": "write"})
	if w := env.doRequestWithToken(t, "DELETE", "/vault/members/leo", nil, token); w.Code != 403 {
		t.Fatalf("service token removed leo: %d", w.Code)
	}
	g := scopedGrant(t, env, sk, "leo:*", "write")
	if w := env.doRequestWithToken(t, "DELETE", "/vault/members/marco", nil, g); w.Code != 403 {
		t.Fatalf("leo:* grant removed marco: %d", w.Code)
	}
	if w := env.doRequestWithToken(t, "DELETE", "/vault/members/leo", nil, g); w.Code != 200 {
		t.Fatalf("approved leo:* grant: %d %s", w.Code, w.Body.String())
	}
	// The grant named leo, so it ends with him.
	if w := env.doRequestWithToken(t, "GET", "/vault/members", nil, g); w.Code != 401 {
		t.Fatalf("leo:* grant after removal: %d", w.Code)
	}
}

func TestMembers_MigrateDryRunValues(t *testing.T) {
	env := setup(t)
	env.doRequest(t, "PUT", "/vault/fields/nucleo_familiare.membro1_nome", map[string]string{"value": "Marco"}, true)
	for _, v := range []string{"1", "true"} {
		w := env.doRequest(t, "POST", "/vault/members/migrate?dry_run="+v, nil, true)
		var report vault.MigrationReport
		json.NewDecoder(w.Body).Decode(&report)
		if w.Code != 200 || !report.DryRun {
			t.Fatalf("dry_run=%s: %d %+v", v, w.Code, report)
		}
	}
	if w := env.doRequest(t, "POST", "/vault/members/migrate?dry_run=yes-please", nil, true); w.Code != 400 {
		t.Fatalf("dry_run=yes-please: %d", w.Code)
	}
	if w := env.doRequest(t, "GET", "/vault/fields/nucleo_familiare.membro1_nome", nil, true); w.Code != 200 {
		t.Fatal("a dry run moved the field")
	}
}

// Exporting a person is for the user's own session only, never a token or a
// scoped grant, even one for everything about that person.
func TestMembers_Export(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	seedHousehold(t, env)
	if w := env.upload(t, env.token, "category=leo:cie&name=fronte.jpg", "image/jpeg", []byte("JPEG")); w.Code != 201 && w.Code != 200 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}

	token := createTokenWithTier(t, env, "*", "critical")
	if w := env.doRequestWithToken(t, "GET", "/vault/members/leo/export", nil, token); w.Code != 403 {
		t.Fatalf("service token exported leo: %d", w.Code)
	}
	g := scopedGrant(t, env, sk, "leo:*", "write")
	if w := env.doRequestWithToken(t, "GET", "/vault/members/leo/export", nil, g); w.Code != 403 {
		t.Fatalf("scoped grant exported leo: %d", w.Code)
	}

	w := env.doRequest(t, "GET", "/vault/members/leo/export", nil, true)
	var out vault.PersonExport
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil || w.Code != 200 {
		t.Fatalf("session export: %d %v", w.Code, err)
	}
	if out.Person != "leo" || len(out.Fields) != 5 || out.Omitted != 1 || len(out.Documents) != 1 || string(out.Documents[0].Data) != "JPEG" {
		t.Fatalf("export = %+v", out)
	}
	w = env.doRequest(t, "GET", "/vault/members/leo/export?include_personal=1", nil, true)
	if !strings.Contains(w.Body.String(), "hunter2-secret") {
		t.Fatal("include_personal left out leo:spid.password")
	}
	if w := env.doRequest(t, "GET", "/vault/members/marco/export", nil, true); w.Code != 404 {
		t.Fatalf("missing person: %d", w.Code)
	}
	if w := env.doRequest(t, "GET", "/vault/members/leo/export?include_personal=maybe", nil, true); w.Code != 400 {
		t.Fatalf("bad include_personal: %d", w.Code)
	}
}

func TestRetention_API(t *testing.T) {
	env := setup(t)
	token := createTokenWithAccess(t, env, "*", "write")
	if w := env.doRequestWithToken(t, "PUT", "/vault/retention", map[string]int{"years": 5}, token); w.Code != 403 {
		t.Fatalf("service token set retention: %d", w.Code)
	}
	if w := env.doRequest(t, "PUT", "/vault/retention", map[string]int{"years": 1000}, true); w.Code != 400 {
		t.Fatalf("1000 years: %d", w.Code)
	}
	if w := env.doRequest(t, "PUT", "/vault/retention", map[string]string{}, true); w.Code != 400 {
		t.Fatalf("no years: %d", w.Code)
	}
	if w := env.doRequest(t, "PUT", "/vault/retention", map[string]int{"years": 10}, true); w.Code != 200 {
		t.Fatalf("session set retention: %d %s", w.Code, w.Body.String())
	}
	w := env.doRequestWithToken(t, "GET", "/vault/retention", nil, token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"years":10`) || strings.Contains(w.Body.String(), "scan_days") {
		t.Fatalf("get retention: %d %s", w.Code, w.Body.String())
	}

	// The period for clients' ID scans: 0 is a period, -1 removes it.
	if w := env.doRequestWithToken(t, "PUT", "/vault/retention", map[string]int{"scan_days": 0}, token); w.Code != 403 {
		t.Fatalf("service token set scan_days: %d", w.Code)
	}
	if w := env.doRequest(t, "PUT", "/vault/retention", map[string]int{"scan_days": vault.MaxScanDays + 1}, true); w.Code != 400 {
		t.Fatalf("too many scan_days: %d", w.Code)
	}
	// Both are checked before either changes.
	if w := env.doRequest(t, "PUT", "/vault/retention", map[string]int{"years": 5, "scan_days": vault.MaxScanDays + 1}, true); w.Code != 400 {
		t.Fatalf("valid years, bad scan_days: %d", w.Code)
	}
	if w := env.doRequest(t, "GET", "/vault/retention", nil, true); !strings.Contains(w.Body.String(), `"years":10`) {
		t.Fatalf("a refused update changed years: %s", w.Body.String())
	}
	if w := env.doRequest(t, "PUT", "/vault/retention", map[string]int{"scan_days": 0}, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"scan_days":0`) || !strings.Contains(w.Body.String(), `"years":10`) {
		t.Fatalf("session set scan_days: %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequest(t, "PUT", "/vault/retention", map[string]int{"scan_days": -1}, true); w.Code != 200 || strings.Contains(w.Body.String(), "scan_days") {
		t.Fatalf("session removed scan_days: %d %s", w.Code, w.Body.String())
	}
}

// A service token naming a person in a field (pratiche persona, famiglia)
// gets the same answer for a person it can't see as for one who doesn't exist,
// so it can't find out who is in the vault. A grant the user approved in
// person (Touch ID: 'pvault set pratiche.x_persona zed') may name anyone.
func TestMembers_HiddenPersonRef(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	// zed exists only through a personal-tier field, which no token sees.
	if w := env.doRequest(t, "PUT", "/vault/fields/zed:spid.password", map[string]string{"value": "hunter2-secret"}, true); w.Code != 200 {
		t.Fatalf("seed zed: %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequest(t, "PUT", "/vault/fields/leo:identity.first_name", map[string]string{"value": "Leo"}, true); w.Code != 200 {
		t.Fatalf("seed leo: %d %s", w.Code, w.Body.String())
	}
	token := createToken(t, env, map[string]string{"scope": "pratiche.*,leo:*", "access": "write"})
	answer := func(slug string) string {
		w := env.doRequestWithToken(t, "PUT", "/vault/fields/pratiche.cie-"+slug+"_persona", map[string]string{"value": slug}, token)
		return strings.ReplaceAll(w.Body.String(), slug, "SLUG") + " " + http.StatusText(w.Code)
	}
	hidden, missing := answer("zed"), answer("nobody")
	if hidden != missing || !strings.Contains(hidden, "Bad Request") {
		t.Fatalf("hidden person: %s\nmissing person: %s", hidden, missing)
	}
	if got := answer("leo"); !strings.HasSuffix(got, " OK") {
		t.Fatalf("visible person: %s", got)
	}
	// An approved grant for just that field, and the session, see everyone.
	g := scopedGrant(t, env, sk, "pratiche.cie-zed_persona", "write")
	if w := env.doRequestWithToken(t, "PUT", "/vault/fields/pratiche.cie-zed_persona", map[string]string{"value": "zed"}, g); w.Code != 200 {
		t.Fatalf("approved grant naming zed: %d %s", w.Code, w.Body.String())
	}
	if w := env.doRequest(t, "PUT", "/vault/fields/pratiche.scia-zed_persona", map[string]string{"value": "zed"}, true); w.Code != 200 {
		t.Fatalf("session naming zed: %d %s", w.Code, w.Body.String())
	}
}

// A grant the user approved for one of a person's fields (what `pvault
// delete leo:cie.numero` asks Touch ID for) deletes it, the last one too:
// leo stays in the vault until 'pvault person remove'.
func TestMembers_DeleteWithApprovedGrant(t *testing.T) {
	env, sk := setupWithSecretKey(t)
	for _, kv := range [][2]string{
		{"leo:identity.first_name", "Leo"},
		{"leo:cie.numero", "CA12345AB"},
	} {
		if w := env.doRequest(t, "PUT", "/vault/fields/"+kv[0], map[string]string{"value": kv[1]}, true); w.Code != 200 {
			t.Fatalf("seed %s: %d %s", kv[0], w.Code, w.Body.String())
		}
	}
	for _, id := range []string{"leo:cie.numero", "leo:identity.first_name"} {
		g := scopedGrant(t, env, sk, id, "write")
		if w := env.doRequestWithToken(t, "DELETE", "/vault/fields/"+id, nil, g); w.Code != 200 {
			t.Fatalf("approved delete of %s: %d %s", id, w.Code, w.Body.String())
		}
	}
	if w := env.doRequest(t, "GET", "/vault/members", nil, true); !strings.Contains(w.Body.String(), `"slug":"leo"`) {
		t.Fatalf("leo gone with their last row: %s", w.Body.String())
	}
}

// A service token can't name a person who isn't in the vault yet.
func TestMembers_TokenForUnknownPerson(t *testing.T) {
	env := setup(t)
	w := env.doRequest(t, "POST", "/vault/tokens/service", map[string]string{"consumer": "agent", "scope": "zed:*", "ttl": "1h"}, true)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `no person \"zed\"`) {
		t.Fatalf("token naming zed: %d %s", w.Code, w.Body.String())
	}
}
