package api

import (
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
	for _, id := range []string{"spid.provider", "preferences.accesso_pa",
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
