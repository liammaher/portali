package vault

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAndValidate_Pratiche(t *testing.T) {
	tests := []struct {
		id, in, want string
	}{
		{"pratiche.tari-iscrizione-2026_stato", "In attesa", "in_attesa"},
		{"pratiche.tari-iscrizione-2026_stato", "da-fare", "da_fare"},
		{"pratiche.tari-iscrizione-2026_canale", " Email ", "email"},
		{"pratiche.tari-iscrizione-2026_data_invio", "21/03/2026", "2026-03-21"},
		{"pratiche.tari-iscrizione-2026_data_scadenza", "2026-12-16", "2026-12-16"},
		{"pratiche.tari-iscrizione-2026_importo", "€ 259", "259.00"},
		{"pratiche.tari-iscrizione-2026_importo", "1.250,50", "1250.50"},
		{"pratiche.tari-iscrizione-2026_titolo", " Mod. 1 TARI ", "Mod. 1 TARI"},
		{"pratiche.tari-iscrizione-2026_destinatario", "entrate@comune.example.it", "entrate@comune.example.it"},
		{"pratiche.cambio-residenza_prossimo_passo", "Wait for the vigile", "Wait for the vigile"},
		{"pratiche.x_playbook", "tari-napoli-iscrizione", "tari-napoli-iscrizione"},
	}
	for _, tt := range tests {
		got, err := NormalizeAndValidate(tt.id, tt.in)
		if err != nil {
			t.Errorf("NormalizeAndValidate(%q, %q) error: %v", tt.id, tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeAndValidate(%q, %q) = %q, want %q", tt.id, tt.in, got, tt.want)
		}
	}
}

func TestNormalizeAndValidate_PraticheRejects(t *testing.T) {
	tests := []struct{ id, in string }{
		{"pratiche.tari_2026_stato", "inviata"},         // underscore in slug
		{"pratiche.stato", "inviata"},                   // no slug
		{"pratiche.tari-2026_colore", "blu"},            // unknown campo
		{"pratiche.tari--2026_stato", "inviata"},        // empty slug word
		{"pratiche.tari-2026_stato", "sent"},            // unknown stato
		{"pratiche.tari-2026_canale", "piccione"},       // unknown canale
		{"pratiche.tari-2026_data_invio", "31/02/2026"}, // impossible date
		{"pratiche.tari-2026_importo", "duecento"},      // not an amount
		{"pratiche.tari-2026_data_scadenza", "presto"},  // not a date
		{"pratiche.TARI-2026_stato", "pronta"},          // upper case would be a second field
	}
	for _, tt := range tests {
		_, err := NormalizeAndValidate(tt.id, tt.in)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("NormalizeAndValidate(%q, %q) = %v, want *ValidationError", tt.id, tt.in, err)
		}
	}
}

// Pratiche rules only apply inside the pratiche category.
func TestPraticheRulesScopedToCategory(t *testing.T) {
	for _, id := range []string{"business.stato", "custom.canale", "identity.tari-2026_stato"} {
		if got, err := NormalizeAndValidate(id, "anything"); err != nil || got != "anything" {
			t.Errorf("NormalizeAndValidate(%q) = %q, %v; want unchanged", id, got, err)
		}
	}
}

func TestSplitPraticaField(t *testing.T) {
	slug, campo, ok := splitPraticaField("tari-iscrizione-2026_data_scadenza")
	if !ok || slug != "tari-iscrizione-2026" || campo != "data_scadenza" {
		t.Errorf("got %q, %q, %v", slug, campo, ok)
	}
	if _, _, ok := splitPraticaField("tari-iscrizione-2026"); ok {
		t.Error("name without campo accepted")
	}
}

// The schema description lists every campo, stato and canale.
func TestPraticheSchemaDescription(t *testing.T) {
	var desc string
	for _, c := range RecommendedSchema.Categories {
		if c.Name == "pratiche" {
			desc = c.Description
		}
	}
	for _, words := range [][]string{praticaCampi, praticaStati, praticaCanali} {
		for _, w := range words {
			if !strings.Contains(desc, w) {
				t.Errorf("pratiche schema description is missing %q", w)
			}
		}
	}
}

func TestVaultSet_Pratiche(t *testing.T) {
	v, _ := tmpVault(t)

	if err := v.Set("pratiche.tari-iscrizione-2026_stato", "pronta", ""); err != nil {
		t.Fatalf("Set stato: %v", err)
	}
	if f, _ := v.Get("pratiche.tari-iscrizione-2026_stato"); f == nil || f.Value != "pronta" || f.Sensitivity != "standard" {
		t.Errorf("stored %+v, want pronta standard", f)
	}

	var verr *ValidationError
	if err := v.Set("pratiche.tari_2026_stato", "pronta", ""); !errors.As(err, &verr) {
		t.Errorf("Set bad slug: got %v, want *ValidationError", err)
	}

	// A pratica's deadline shows up in the expiry tracker under its slug.
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	if err := v.Set("pratiche.tari-iscrizione-2026_data_scadenza", "16/12/2026", ""); err != nil {
		t.Fatal(err)
	}
	exp, err := v.Expiring(90*24*time.Hour, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(exp) != 1 || exp[0].FieldID != "pratiche.tari-iscrizione-2026_data_scadenza" ||
		exp[0].Document != "pratica tari-iscrizione-2026" || exp[0].DaysLeft != 52 {
		t.Errorf("Expiring = %+v, want pratica tari-iscrizione-2026 in 52 days", exp)
	}

	// Once the pratica is closed its deadline is no longer flagged.
	if err := v.Set("pratiche.tari-iscrizione-2026_stato", "completata", ""); err != nil {
		t.Fatal(err)
	}
	if exp, err := v.Expiring(90*24*time.Hour, nil, now); err != nil || len(exp) != 0 {
		t.Errorf("Expiring after completata = %+v, %v; want none", exp, err)
	}
}

// A slug that mentions the CIE PIN doesn't make the pratica a credential;
// real credential fields still are.
func TestPraticheNotCredentials(t *testing.T) {
	for _, id := range []string{"pratiche.cie-pin-ristampa_stato", "pratiche.spid-password-reset_note"} {
		if IsCredentialField(id) {
			t.Errorf("IsCredentialField(%q) = true, want false", id)
		}
		if got := DefaultSensitivity(id); got != "standard" {
			t.Errorf("DefaultSensitivity(%q) = %q, want standard", id, got)
		}
	}
	for _, id := range []string{"cie.pin", "identity.spid_password"} {
		if !IsCredentialField(id) {
			t.Errorf("IsCredentialField(%q) = false, want true", id)
		}
	}
}

func TestIsClosedStato(t *testing.T) {
	for _, s := range []string{"completata", " Completata ", "respinta", "ANNULLATA"} {
		if !isClosedStato(s) {
			t.Errorf("isClosedStato(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"in_attesa", "inviata", ""} {
		if isClosedStato(s) {
			t.Errorf("isClosedStato(%q) = true, want false", s)
		}
	}
}
