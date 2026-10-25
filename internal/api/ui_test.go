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
