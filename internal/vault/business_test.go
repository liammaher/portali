package vault

import (
	"errors"
	"testing"
)

func TestNormalizeAndValidate_Business(t *testing.T) {
	tests := []struct {
		id, in, want string
	}{
		{"business.qualita_dichiarante", " Titolare ", "titolare"},
		{"business.qualita_dichiarante", "Legale rappresentante", "legale_rappresentante"},
		{"business.qualita_dichiarante", "rappresentante-legale", "legale_rappresentante"},
		{"business.sede_legale_cap", " 80134 ", "80134"},
		{"business.sede_legale_provincia", "na", "NA"},
		{"locale.cap", "80125", "80125"},
		{"locale.provincia", " Na ", "NA"},
		{"locale.interno", "int. 3", "3"},
		{"locale.titolo", " Conduttore ", "locatario"},
		{"locale.titolo", "proprietaria", "proprietario"},
		{"locale.data_inizio", "01/03/2026", "2026-03-01"},
		{"locale.catasto_categoria", "c1", "C/1"},
		{"locale.catasto_foglio", " 12 ", "12"},
		{"locale.catasto_particella", "34b", "34B"},
		{"locale.catasto_subalterno", "5", "5"},
		{"locale.superficie_mq", "45,5 mq", "45.5"},
		{"locale.superficie_mq", "2.500 m²", "2500"},
		{"identity.birth_country", " Regno Unito ", "Regno Unito"},
		{"residenza.stato", " Regno Unito ", "Regno Unito"},
		{"business.sede_legale_stato", "Francia", "Francia"},
		{"locale.tari_superficie_tassata_mq", "45,5", "45.5"},
		{"locale.tari_data_dichiarazione", "01/03/2026", "2026-03-01"},
		// A second premises follows locale's rules.
		{"locale2.tari_superficie_tassata_mq", "1.200", "1200"},
		{"locale2.cap", "80125", "80125"},
		// Generic rules still apply in business.
		{"business.partita_iva", "IT 00743110157", "00743110157"},
		{"business.data_apertura", "02/01/2025", "2025-01-02"},
		// No rule: unchanged.
		{"business.email", "info@example.it", "info@example.it"},
		{"business.sede_legale_comune", "Napoli", "Napoli"},
		{"locale.destinazione_uso", "negozio", "negozio"},
		{"locale.catasto_sezione", "SFE", "SFE"},
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

func TestNormalizeAndValidate_BusinessRejects(t *testing.T) {
	tests := []struct{ id, in string }{
		{"business.qualita_dichiarante", "socio"},
		{"business.sede_legale_cap", "8013"},
		{"business.sede_legale_provincia", "Napoli"},
		{"locale.cap", "80 125"},
		{"locale.provincia", "N1"},
		{"locale.interno", "int."},
		{"locale.titolo", "ospite"}, // fine for a home, not for business premises
		{"locale.data_inizio", "31/02/2026"},
		{"locale.catasto_categoria", "C/8"},
		{"locale.catasto_foglio", "12a"},
		{"locale.superficie_mq", "0"},
		{"identity.birth_country", "Italia"},
		{"identity.birth_country", " italy "},
		// Empty is Italia: the country fields are for an address abroad.
		{"residenza.stato", "Italia"},
		{"business.sede_legale_stato", " ITALY "},
		{"locale.tari_superficie_tassata_mq", "tanti"},
		{"locale2.tari_data_dichiarazione", "31/02/2026"},
	}
	for _, tt := range tests {
		_, err := NormalizeAndValidate(tt.id, tt.in)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("NormalizeAndValidate(%q, %q) = %v, want *ValidationError", tt.id, tt.in, err)
		}
	}
}

// Business rules only apply to their own fields: a sede legale field outside
// business, or a catasto field outside a unit category, is left alone.
func TestBusinessRulesScopedToCategory(t *testing.T) {
	for _, id := range []string{"identity.sede_legale_cap", "custom.qualita_dichiarante", "employment.catasto_foglio", "business.titolo"} {
		if got, err := NormalizeAndValidate(id, "anything"); err != nil || got != "anything" {
			t.Errorf("NormalizeAndValidate(%q) = %q, %v; want unchanged", id, got, err)
		}
	}
}

func TestVaultSet_Locale(t *testing.T) {
	v, _ := tmpVault(t)

	if err := v.Set("locale.catasto_categoria", "c 1", ""); err != nil {
		t.Fatalf("Set catasto_categoria: %v", err)
	}
	if f, _ := v.Get("locale.catasto_categoria"); f == nil || f.Value != "C/1" || f.Sensitivity != "standard" {
		t.Errorf("stored %+v, want C/1 standard", f)
	}
	var verr *ValidationError
	if err := v.Set("business.sede_legale_cap", "801", ""); !errors.As(err, &verr) {
		t.Errorf("Set bad CAP: got %v, want *ValidationError", err)
	}
}
