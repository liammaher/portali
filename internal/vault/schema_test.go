package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestIsCanonicalField(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"identity.full_name", true},
		{"identity.email", true},
		{"payment.card_number", true},
		{"financial.ssn", true},
		{"identity.nickname", false},
		{"custom.field", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsCanonicalField(tt.id); got != tt.want {
			t.Errorf("IsCanonicalField(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestGetSchemaField(t *testing.T) {
	f := GetSchemaField("identity.full_name")
	if f == nil {
		t.Fatal("expected non-nil for identity.full_name")
	}
	if f.Description == "" {
		t.Error("expected non-empty description")
	}

	if GetSchemaField("nonexistent.field") != nil {
		t.Error("expected nil for nonexistent field")
	}
}

func TestDefaultSensitivity(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"identity.full_name", "standard"},
		{"identity.phone", "sensitive"},
		{"financial.ssn", "critical"},
		{"payment.card_number", "critical"},
		{"preferences.timezone", "public"},
		{"custom.whatever", "standard"},
	}
	for _, tt := range tests {
		if got := DefaultSensitivity(tt.id); got != tt.want {
			t.Errorf("DefaultSensitivity(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestSchemaIntegrity(t *testing.T) {
	seen := make(map[string]bool)
	for _, cat := range RecommendedSchema.Categories {
		if cat.Name == "" {
			t.Error("category with empty name")
		}
		for _, f := range cat.Fields {
			if seen[f.ID] {
				t.Errorf("duplicate field ID: %s", f.ID)
			}
			seen[f.ID] = true

			if err := ValidateFieldID(f.ID); err != nil {
				t.Errorf("invalid field ID in schema: %s: %v", f.ID, err)
			}
			if f.Description == "" {
				t.Errorf("field %s has empty description", f.ID)
			}
			if !validTiers[f.Sensitivity] {
				t.Errorf("field %s has invalid sensitivity %q", f.ID, f.Sensitivity)
			}
		}
	}
}

// one_only tells tools that read schema.json (pratiche-napoli's validator)
// which categories have no numbered copies, the ones NormalizeAndValidate refuses.
func TestSchemaOneOnlyIsTheVaultsOwn(t *testing.T) {
	for _, c := range RecommendedSchema.Categories {
		_, err := NormalizeAndValidate("x:"+c.Name+"2.y", "z")
		refused := err != nil && strings.Contains(err.Error(), "no numbered copies")
		if c.OneOnly != refused {
			t.Errorf("%s: one_only %v, but a numbered copy refused %v (%v)", c.Name, c.OneOnly, refused, err)
		}
	}
}

func TestSchemaIndex_MatchesData(t *testing.T) {
	count := 0
	for _, cat := range RecommendedSchema.Categories {
		count += len(cat.Fields)
	}
	if len(schemaIndex) != count {
		t.Errorf("schemaIndex has %d entries, but schema has %d fields", len(schemaIndex), count)
	}
}

// The exported credential words are the expressions the vault compiles, so a
// tool reading them refuses exactly the words IsCredentialField reads.
func TestSchemaCredentialWordsAreTheVaultsOwn(t *testing.T) {
	words := RecommendedSchema.Credentials
	for _, tt := range []struct {
		exported string
		re       *regexp.Regexp
	}{
		{words.Secret, reSecretWord},
		{words.Credential, reCredentialWord},
		{words.Identity, reIdentityWord},
	} {
		if regexp.MustCompile(tt.exported).String() != tt.re.String() {
			t.Errorf("exported %q, the vault compiles %q", tt.exported, tt.re)
		}
	}
	for _, w := range []string{"pin", "pincode", "credenziali", "sblocco", "passwords", "password2"} {
		if !regexp.MustCompile(words.Secret).MatchString(w) || !regexp.MustCompile(words.Credential).MatchString(w) {
			t.Errorf("%s should be a secret word", w)
		}
	}
	if regexp.MustCompile(words.Secret).MatchString("username") || !regexp.MustCompile(words.Credential).MatchString("username") {
		t.Error("username is a credential word only next to an identity word")
	}
}

// schema.json is what pratiche-napoli's CI compares its copy with, so it
// must be exactly what 'pvault schema --json' prints.
func TestSchemaJSONIsCommitted(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join("..", "..", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, SchemaJSON()) {
		t.Fatal("schema.json is out of date: run 'make schema'")
	}
}
