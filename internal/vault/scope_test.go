package vault

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestScopeAllows(t *testing.T) {
	tests := []struct {
		scope   string
		fieldID string
		want    bool
	}{
		// Wildcard
		{"*", "identity.full_name", true},
		{"*", "financial.income", true},

		// Category wildcard
		{"identity.*", "identity.full_name", true},
		{"identity.*", "identity.dob", true},
		{"identity.*", "financial.income", false},

		// Exact field
		{"identity.full_name", "identity.full_name", true},
		{"identity.full_name", "identity.dob", false},
		{"identity.full_name", "financial.income", false},

		// Multiple patterns
		{"identity.*,financial.*", "identity.full_name", true},
		{"identity.*,financial.*", "financial.income", true},
		{"identity.*,financial.*", "addresses.home_city", false},

		// Mixed patterns
		{"identity.*,financial.income", "financial.income", true},
		{"identity.*,financial.income", "financial.ssn", false},

		// Whitespace in patterns
		{"identity.* , financial.*", "financial.income", true},

		// Empty/edge cases
		{"", "identity.full_name", false},
		{"identity.*", "", false},
	}

	for _, tt := range tests {
		got := ScopeAllows(tt.scope, tt.fieldID)
		if got != tt.want {
			t.Errorf("ScopeAllows(%q, %q) = %v, want %v", tt.scope, tt.fieldID, got, tt.want)
		}
	}
}

func TestValidateFieldID(t *testing.T) {
	valid := []string{
		"identity.full_name",
		"financial.income",
		"addresses.home_city",
		"identity.t-shirt_size",
		"my_category.my_field",
		"A.B",
		"leo:identity.full_name",
		"maria-luisa:cie.numero",
		"leo2:spid_poste.password",
	}
	for _, id := range valid {
		if err := ValidateFieldID(id); err != nil {
			t.Errorf("ValidateFieldID(%q) = %v, want nil", id, err)
		}
	}

	invalid := []string{
		"",
		"noDot",
		".leading_dot",
		"trailing_dot.",
		"identity.full name",       // space
		"identity.full/name",       // slash
		"../etc/passwd.evil",       // path traversal
		"identity.name\x00extra",   // null byte
		"identity.name;DROP TABLE", // SQL injection attempt
		"cat.field.extra",          // three parts still valid as SplitN(2) keeps "field.extra"
		":identity.full_name",      // empty member
		"io:identity.full_name",    // reserved for the owner
		"Leo:identity.full_name",   // members are lower-case
		"leo:cie:x.y",              // two prefixes
		"leo_x:cie.numero",         // underscore in slug
		"leo:.numero",
		"leo:cie.",
	}
	for _, id := range invalid {
		if err := ValidateFieldID(id); err == nil {
			t.Errorf("ValidateFieldID(%q) = nil, want error", id)
		}
	}
}

func TestScopeAllowsCategory(t *testing.T) {
	tests := []struct {
		scope    string
		category string
		want     bool
	}{
		{"*", "identity", true},
		{"identity.*", "identity", true},
		{"identity.*", "financial", false},
		{"identity.*,financial.*", "financial", true},
		{"identity.*,financial.*", "addresses", false},
		{"identity.full_name", "identity", true},
		{"identity.full_name", "financial", false},
		{"", "identity", false},
	}

	for _, tt := range tests {
		got := ScopeAllowsCategory(tt.scope, tt.category)
		if got != tt.want {
			t.Errorf("ScopeAllowsCategory(%q, %q) = %v, want %v", tt.scope, tt.category, got, tt.want)
		}
	}
}

// scopeFixture is testdata/scopes.json, shared with mcp/src/scope.test.ts.
type scopeFixture struct {
	Fields []struct {
		Scope, ID string
		Want      bool
	}
	Categories []struct {
		Scope, Category string
		Want            bool
	}
	People []struct {
		Scope, Person string
		Want          bool
	}
	ValidScopes   []string
	InvalidScopes []string
}

func loadScopeFixture(t *testing.T) scopeFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/scopes.json")
	if err != nil {
		t.Fatal(err)
	}
	var f scopeFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestScopeFixture(t *testing.T) {
	f := loadScopeFixture(t)
	for _, c := range f.Fields {
		if got := ScopeAllows(c.Scope, c.ID); got != c.Want {
			t.Errorf("ScopeAllows(%q, %q) = %v, want %v", c.Scope, c.ID, got, c.Want)
		}
	}
	for _, c := range f.Categories {
		if got := ScopeAllowsCategory(c.Scope, c.Category); got != c.Want {
			t.Errorf("ScopeAllowsCategory(%q, %q) = %v, want %v", c.Scope, c.Category, got, c.Want)
		}
	}
	for _, c := range f.People {
		if got := ScopeAllowsPerson(c.Scope, c.Person); got != c.Want {
			t.Errorf("ScopeAllowsPerson(%q, %q) = %v, want %v", c.Scope, c.Person, got, c.Want)
		}
	}
	for _, s := range f.ValidScopes {
		if err := ValidateScope(s); err != nil {
			t.Errorf("ValidateScope(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range f.InvalidScopes {
		if err := ValidateScope(s); err == nil {
			t.Errorf("ValidateScope(%q) = nil, want error", s)
		}
	}
}

func TestSplitPerson(t *testing.T) {
	tests := []struct{ id, member, local string }{
		{"cie.numero", "", "cie.numero"},
		{"leo:cie.numero", "leo", "cie.numero"},
		{"Leo:spid.password", "Leo", "spid.password"},
		{"leo:cie", "leo", "cie"},
		{"cie", "", "cie"},
		{"cie.a:b", "", "cie.a:b"}, // a colon after the dot isn't a prefix
	}
	for _, tt := range tests {
		m, l := SplitPerson(tt.id)
		if m != tt.member || l != tt.local {
			t.Errorf("SplitPerson(%q) = %q, %q; want %q, %q", tt.id, m, l, tt.member, tt.local)
		}
	}
}

func TestScopeNamesPerson(t *testing.T) {
	tests := []struct {
		scope, member string
		want          bool
	}{
		{"leo:*", "leo", true},
		{"identity.*,leo:cie.*", "leo", true},
		{"*", "leo", false},
		{"*:identity.*", "leo", false},
		{"leonardo:*", "leo", false},
	}
	for _, tt := range tests {
		if got := ScopeNamesPerson(tt.scope, tt.member); got != tt.want {
			t.Errorf("ScopeNamesPerson(%q, %q) = %v, want %v", tt.scope, tt.member, got, tt.want)
		}
	}
}

func TestValidCategoryName(t *testing.T) {
	for _, c := range []string{"cie", "spid_poste", "leo:cie", "maria-luisa:identity"} {
		if !ValidCategoryName(c) {
			t.Errorf("ValidCategoryName(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"", "leo:", ":cie", "io:cie", "Leo:cie", "leo:cie.x", "a:b:c", "cie/x"} {
		if ValidCategoryName(c) {
			t.Errorf("ValidCategoryName(%q) = true, want false", c)
		}
	}
}

// New fields and document categories must be lower-case, so one pratica or
// ruolo can't be stored twice under two spellings; rows from before the rule
// can still be read, re-tiered and deleted.
func TestLowerCaseIDs(t *testing.T) {
	v, _ := tmpVault(t)
	for _, id := range []string{"Pratiche.cila-roma_stato", "identity.First_name", "leo:Cie.numero", "custom.X"} {
		var verr *ValidationError
		if err := v.Set(id, "x", ""); !errors.As(err, &verr) || !strings.Contains(err.Error(), strings.ToLower(id)) {
			t.Errorf("Set(%q) = %v, want a ValidationError naming the lower-case ID", id, err)
		}
	}
	if _, err := v.AddDocument("Cie", "cie.pdf", "application/pdf", []byte("%PDF"), ""); err == nil {
		t.Error("AddDocument stored an upper-case category")
	}

	setLegacy(t, v, "custom.Old", "kept")
	if f, err := v.Get("custom.Old"); err != nil || f == nil || f.Value != "kept" {
		t.Fatalf("Get(legacy) = %+v, %v", f, err)
	}
	if err := v.SetSensitivity("custom.Old", "sensitive"); err != nil {
		t.Fatalf("SetSensitivity(legacy) = %v", err)
	}
	if err := v.Delete("custom.Old"); err != nil {
		t.Fatalf("Delete(legacy) = %v", err)
	}
}
