package vault

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeAndValidate_Housing(t *testing.T) {
	tests := []struct {
		id, in, want string
	}{
		{"abitazione.titolo", " Inquilino ", "locatario"},
		{"abitazione.titolo", "proprietario", "proprietario"},
		{"abitazione.catasto_categoria", "a2", "A/2"},
		{"abitazione.catasto_categoria", "A/03", "A/3"},
		{"abitazione.catasto_categoria", "c / 6", "C/6"},
		{"abitazione.catasto_categoria", "A/11", "A/11"},
		{"abitazione.catasto_foglio", " 123 ", "123"},
		{"abitazione.catasto_particella", "456a", "456A"},
		{"abitazione.catasto_subalterno", "7", "7"},
		{"abitazione.classe_energetica", "classe a4", "A4"},
		{"abitazione.classe_energetica", "g", "G"},
		{"abitazione.superficie_calpestabile_mq", "72,5 mq", "72.5"},
		{"abitazione.superficie_catastale_mq", "80", "80"},
		{"abitazione.vani", "4,5", "4.5"},
		{"tari.superficie_tassata_mq", "65 m²", "65"},
		{"abitazione.catasto_rendita", "€ 512,30", "512.30"},
		{"locazione.canone_mensile", "750", "750.00"},
		{"locazione.canone_mensile", "1.250,50", "1250.50"},
		{"locazione.canone_mensile", "1,250.50", "1250.50"},
		{"locazione.deposito_cauzionale", "1.500", "1500.00"}, // Italian thousands dot
		{"locazione.deposito_cauzionale", "1.000.000,00", "1000000.00"},
		{"locazione.deposito_cauzionale", "12.500", "12500.00"},
		{"abitazione.titolo", "Inquilina", "locatario"},
		{"abitazione.titolo", "comodataria", "comodatario"},
		{"abitazione.titolo", "usufruttuaria", "usufruttuario"},
		{"locazione.spese_condominiali_mensili", "45.5 euro", "45.50"},
		{"locazione.preavviso_disdetta_mesi", " 6 ", "6"},
		{"tari.componenti_tassati", "2", "2"},
		{"locazione.cedolare_secca", "Sì", "si"},
		{"locazione.cedolare_secca", "NO", "no"},
		{"locazione.registrazione_codice_identificativo", "tnm23t 001234 000lh", "TNM23T001234000LH"},
		{"utenze.luce_pod", "it 001 e 12345678", "IT001E12345678"},
		{"utenze.luce_pod", "IT012E123456789", "IT012E123456789"},
		{"utenze.gas_pdr", "0034 1234 5678 90", "00341234567890"},
		// Generic rules still apply inside housing categories.
		{"locazione.locatore_codice_fiscale", "rssmra85t10a562s", "RSSMRA85T10A562S"},
		{"locazione.locatore_iban", "IT60 X054 2811 1010 0000 0123 456", "IT60X0542811101000000123456"},
		{"locazione.data_scadenza", "31/12/2029", "2029-12-31"},
		{"locazione.data_registrazione", "15/03/2026", "2026-03-15"},
		// No rule: unchanged.
		{"utenze.luce_fornitore", " Enel Energia ", " Enel Energia "},
		{"locazione.tipo_contratto", "4+4 libero", "4+4 libero"},
		{"tari.codice_contribuente", "0012345", "0012345"},
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

func TestNormalizeAndValidate_HousingRejects(t *testing.T) {
	tests := []struct{ id, in string }{
		{"abitazione.titolo", "squatter"},
		{"abitazione.catasto_categoria", "A/12"},
		{"abitazione.catasto_categoria", "C/8"},
		{"abitazione.catasto_categoria", "G/1"},
		{"abitazione.catasto_categoria", "A/0"},
		{"abitazione.catasto_foglio", "12a"},
		{"abitazione.catasto_subalterno", "sub 7"},
		{"abitazione.catasto_particella", "12345678"},
		{"abitazione.classe_energetica", "A+"},
		{"abitazione.superficie_calpestabile_mq", "0"},
		{"abitazione.superficie_calpestabile_mq", "about 70"},
		{"abitazione.vani", "-2"},
		{"locazione.canone_mensile", "seven hundred"},
		{"locazione.canone_mensile", "1e3"},
		{"locazione.canone_mensile", "-750"},
		// A mistyped separator must not store a different amount.
		{"locazione.canone_mensile", "1.234.56"},
		{"locazione.canone_mensile", "1,234,56"},
		{"locazione.canone_mensile", "1.2.3"},
		{"locazione.canone_mensile", "12.34,56"},
		{"locazione.canone_mensile", "0.500"},
		{"locazione.canone_mensile", "259,999"},
		{"locazione.canone_mensile", "1,250"},
		{"locazione.canone_mensile", "1.000,000.00"},
		{"locazione.preavviso_disdetta_mesi", "25"},
		{"locazione.preavviso_disdetta_mesi", "3.5"},
		{"tari.componenti_tassati", "0"},
		{"locazione.cedolare_secca", "maybe"},
		{"locazione.registrazione_codice_identificativo", "ABC"},
		{"utenze.luce_pod", "IT001X12345678"},
		{"utenze.luce_pod", "FR001E12345678"},
		{"utenze.gas_pdr", "1234567890123"},
		{"locazione.locatore_iban", "IT60X0542811101000000123457"},
		{"locazione.data_scadenza", "31/02/2029"},
	}
	for _, tt := range tests {
		_, err := NormalizeAndValidate(tt.id, tt.in)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("NormalizeAndValidate(%q, %q) = %v, want *ValidationError", tt.id, tt.in, err)
		}
	}
}

// Housing rules only apply inside the housing categories.
func TestHousingRulesScopedToCategory(t *testing.T) {
	for _, id := range []string{"business.titolo", "preferences.vani", "custom.superficie_mq", "identity.luce_pod"} {
		if got, err := NormalizeAndValidate(id, "anything"); err != nil || got != "anything" {
			t.Errorf("NormalizeAndValidate(%q) = %q, %v; want unchanged", id, got, err)
		}
	}
}

func TestDefaultSensitivity_Housing(t *testing.T) {
	tests := []struct{ id, want string }{
		{"abitazione.catasto_foglio", "standard"},
		{"abitazione.superficie_calpestabile_mq", "standard"},
		{"locazione.canone_mensile", "sensitive"},
		{"locazione.locatore_iban", "sensitive"},
		{"locazione.locatore_codice_fiscale", "sensitive"},
		{"locazione.data_scadenza", "standard"},
		{"utenze.luce_pod", "standard"},
		{"utenze.luce_codice_cliente", "sensitive"},
		{"tari.codice_contribuente", "standard"},
	}
	for _, tt := range tests {
		if got := DefaultSensitivity(tt.id); got != tt.want {
			t.Errorf("DefaultSensitivity(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestSuggestCanonical_Housing(t *testing.T) {
	tests := []struct{ id, want string }{
		{"utenze.pod", "utenze.luce_pod"},
		{"utenze.pdr", "utenze.gas_pdr"},
		{"locazione.affitto", "locazione.canone_mensile"},
		{"locazione.cauzione", "locazione.deposito_cauzionale"},
		{"locazione.scadenza", "locazione.data_scadenza"},
		{"locazione.landlord", "locazione.locatore_nome"},
		{"abitazione.foglio", "abitazione.catasto_foglio"},
		{"abitazione.categoria_catastale", "abitazione.catasto_categoria"},
		{"abitazione.rendita", "abitazione.catasto_rendita"},
		{"abitazione.ape", "abitazione.classe_energetica"},
	}
	for _, tt := range tests {
		s := SuggestCanonical(tt.id)
		if s == nil || s.Canonical != tt.want {
			t.Errorf("SuggestCanonical(%q) = %+v, want %q", tt.id, s, tt.want)
		}
	}
}

func TestVaultSet_Housing(t *testing.T) {
	v, _ := tmpVault(t)

	if err := v.Set("abitazione.catasto_categoria", "a3", ""); err != nil {
		t.Fatalf("Set catasto_categoria: %v", err)
	}
	if f, _ := v.Get("abitazione.catasto_categoria"); f == nil || f.Value != "A/3" || f.Sensitivity != "standard" {
		t.Errorf("stored %+v, want A/3 standard", f)
	}

	if err := v.Set("locazione.canone_mensile", "750,00", ""); err != nil {
		t.Fatalf("Set canone_mensile: %v", err)
	}
	if f, _ := v.Get("locazione.canone_mensile"); f == nil || f.Value != "750.00" || f.Sensitivity != "sensitive" {
		t.Errorf("stored %+v, want 750.00 sensitive", f)
	}

	var verr *ValidationError
	if err := v.Set("utenze.gas_pdr", "123", ""); !errors.As(err, &verr) {
		t.Errorf("Set bad PDR: got %v, want *ValidationError", err)
	}

	// The lease end date is picked up by the expiry tracker.
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	if err := v.Set("locazione.data_scadenza", "30/11/2026", ""); err != nil {
		t.Fatal(err)
	}
	exp, err := v.Expiring(90*24*time.Hour, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(exp) != 1 || exp[0].FieldID != "locazione.data_scadenza" || exp[0].Document != "locazione" || exp[0].DaysLeft != 36 {
		t.Errorf("Expiring = %+v, want locazione.data_scadenza in 36 days", exp)
	}
}
