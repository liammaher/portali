package api

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// Every field on the onboarding page must belong to a category the page
// loads, or a saved value would show up empty on the next visit.
func TestOnboardingFieldsAreLoaded(t *testing.T) {
	page := string(onboardingHTML)
	list := regexp.MustCompile(`(?s)const categories = \[(.*?)\];`).FindStringSubmatch(page)
	if list == nil {
		t.Fatal("categories list not found in onboarding.html")
	}
	loaded := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(list[1], -1) {
		loaded[m[1]] = true
	}

	fields := regexp.MustCompile(`data-field="([a-z0-9_]+\.[a-z0-9_]+)"`).FindAllStringSubmatch(page, -1)
	if len(fields) == 0 {
		t.Fatal("no data-field inputs found")
	}
	for _, m := range fields {
		id := m[1]
		category := strings.SplitN(id, ".", 2)[0]
		if !loaded[category] {
			t.Errorf("%s: category %q is not in the page's categories list", id, category)
		}
		if vault.IsHousingCategory(category) && !vault.IsCanonicalField(id) {
			t.Errorf("%s is not in the schema", id)
		}
	}
}

// Every option of a validated select on the onboarding page must be stored as
// written, or picking it would fail or save a different spelling.
func TestOnboardingSelectOptionsValidate(t *testing.T) {
	page := string(onboardingHTML)
	// The famiglia selects show a family member's fields, so they are
	// checked as a member's (x:famiglia.relazione).
	for _, id := range []string{"spid.provider", "preferences.accesso_pa", "business.forma_giuridica", "locale.titolo",
		"famiglia.relazione", "famiglia.stato_famiglia", "famiglia.a_carico_percentuale", "famiglia.rappresentanza"} {
		sel := regexp.MustCompile(`(?s)<select data-field="` + regexp.QuoteMeta(id) + `">(.*?)</select>`).FindStringSubmatch(page)
		if sel == nil {
			t.Errorf("%s: select not found in onboarding.html", id)
			continue
		}
		stored := id
		if strings.HasPrefix(id, "famiglia.") {
			stored = "x:" + id
		}
		for _, m := range regexp.MustCompile(`<option value="([^"]+)">`).FindAllStringSubmatch(sel[1], -1) {
			got, err := vault.NormalizeAndValidate(stored, m[1])
			if err != nil || got != m[1] {
				t.Errorf("%s option %q normalizes to %q, %v; want it unchanged", id, m[1], got, err)
			}
		}
	}
}

// The page offers every relazione the vault accepts.
func TestOnboardingRelazioniComplete(t *testing.T) {
	page := string(onboardingHTML)
	sel := regexp.MustCompile(`(?s)<select data-field="famiglia.relazione">(.*?)</select>`).FindStringSubmatch(page)
	if sel == nil {
		t.Fatal("relazione select not found")
	}
	var offered []string
	for _, m := range regexp.MustCompile(`<option value="([^"]+)">`).FindAllStringSubmatch(sel[1], -1) {
		offered = append(offered, m[1])
	}
	schema := vault.GetSchemaField("famiglia.relazione").Description
	for _, key := range offered {
		if !strings.Contains(schema, key) {
			t.Errorf("option %q not in the schema's relazioni", key)
		}
	}
	if got, want := len(offered), strings.Count(strings.SplitN(strings.SplitN(schema, ": ", 2)[1], " (", 2)[0], ",")+1; got != want {
		t.Errorf("page offers %d relazioni, the vault accepts %d", got, want)
	}
}

// The Persone section offers every ruolo the vault accepts, as checkboxes
// (stored as a person's ruolo.tipo) and in the add-person form, each stored
// as written.
func TestOnboardingRuoliComplete(t *testing.T) {
	page := string(onboardingHTML)
	box := regexp.MustCompile(`(?s)<div class="checks" data-checks="ruolo.tipo">(.*?)</div>`).FindStringSubmatch(page)
	add := regexp.MustCompile(`(?s)<select id="addRuolo">(.*?)</select>`).FindStringSubmatch(page)
	if box == nil || add == nil {
		t.Fatal("ruolo checkboxes or add-person select not found")
	}
	schema := vault.GetSchemaField("ruolo.tipo").Description
	listed := strings.SplitN(strings.SplitN(schema, ": ", 2)[1], " (", 2)[0]
	want := strings.Split(listed, ", ")
	for name, part := range map[string]string{"checkboxes": box[1], "add-person select": add[1]} {
		var offered []string
		for _, m := range regexp.MustCompile(`value="([^"]+)"`).FindAllStringSubmatch(part, -1) {
			offered = append(offered, m[1])
			if got, err := vault.NormalizeAndValidate("x:ruolo.tipo", m[1]); err != nil || got != m[1] {
				t.Errorf("%s: %q normalizes to %q, %v", name, m[1], got, err)
			}
		}
		if strings.Join(offered, ",") != strings.Join(want, ",") {
			t.Errorf("%s offer %v, the vault accepts %v", name, offered, want)
		}
	}
}

// Sections the page shows or hides by vault kind and category mirror the
// schema: every data-kind is a vault kind, every data-relation a category
// kept for some kinds.
func TestOnboardingKindMarkers(t *testing.T) {
	page := string(onboardingHTML)
	for _, m := range regexp.MustCompile(`data-kind="([a-z]+)"`).FindAllStringSubmatch(page, -1) {
		if m[1] != vault.KindPersona && m[1] != vault.KindImpresa {
			t.Errorf("data-kind %q", m[1])
		}
	}
	for _, m := range regexp.MustCompile(`data-relation="([a-z_]+)"`).FindAllStringSubmatch(page, -1) {
		if c := vault.GetSchemaCategory(m[1]); c == nil || len(c.Kinds) == 0 {
			t.Errorf("data-relation %q is not a kind-specific category", m[1])
		}
	}
}

// The onboarding page shows what the vault says each field's holder can
// have (/ui/fields), for every input on it and in both kinds of vault.
func TestUIFields(t *testing.T) {
	persona := setup(t)
	dir := filepath.Join(t.TempDir(), ".pvault")
	sk, err := vault.InitWithOptions(dir, testPassword, vault.InitOptions{Kind: vault.KindImpresa})
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	if _, err := v.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	impresa := &testEnv{server: New(v, ":0"), vault: v}

	type holders struct{ Owner, Person bool }
	get := func(env *testEnv) (fields, categories map[string]holders) {
		t.Helper()
		w := env.doRequest(t, "GET", "/ui/fields", nil, false)
		var out struct{ Fields, Categories map[string]holders }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("/ui/fields: %d %s", w.Code, w.Body.String())
		}
		return out.Fields, out.Categories
	}
	pf, pc := get(persona)
	bf, bc := get(impresa)
	if len(pf) != len(pageFields) || len(bf) != len(pageFields) {
		t.Fatalf("answers for %d and %d of the page's %d fields", len(pf), len(bf), len(pageFields))
	}
	for _, c := range []struct {
		got  holders
		want holders
		what string
	}{
		{pf["identity.first_name"], holders{true, true}, "persona identity.first_name"},
		{pf["famiglia.relazione"], holders{false, true}, "persona famiglia.relazione"},
		{pf["abitazione.titolo"], holders{true, false}, "persona abitazione.titolo"},
		{pf["procura.ente"], holders{false, true}, "persona procura.ente"},
		{pf["cie.pin"], holders{true, true}, "persona cie.pin"},
		{bf["identity.first_name"], holders{false, true}, "impresa identity.first_name"},
		{bf["business.denominazione"], holders{true, true}, "impresa business.denominazione"},
		{bf["abitazione.titolo"], holders{false, false}, "impresa abitazione.titolo"},
		{bf["cie.pin"], holders{false, false}, "impresa cie.pin"},
		{bf["spid.provider"], holders{true, false}, "impresa spid.provider"},
		{pc["procura"], holders{false, true}, "persona procura category"},
		{bc["famiglia"], holders{false, false}, "impresa famiglia category"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %+v, want %+v", c.what, c.got, c.want)
		}
	}
}

// The page holds a grant and shows decrypted values: it loads nothing from
// elsewhere, sends no Referer, and runs only the script the server shipped.
func TestOnboardingPageIsLockedDown(t *testing.T) {
	env := setup(t)
	w := env.doRequest(t, "GET", "/ui", nil, false)
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'sha256-", "style-src 'sha256-", "connect-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if scriptSrc := regexp.MustCompile(`script-src [^;]*`).FindString(csp); strings.Contains(scriptSrc, "unsafe") {
		t.Errorf("script-src allows inline script: %q", scriptSrc)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
	if ext := regexp.MustCompile(`(?:src|href)="https?://`).FindString(w.Body.String()); ext != "" {
		t.Errorf("the page loads %s...: it must be self-contained", ext)
	}
}
