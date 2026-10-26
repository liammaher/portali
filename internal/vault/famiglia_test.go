package vault

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFamiglia_Normalize(t *testing.T) {
	tests := []struct{ id, in, want string }{
		{"leo:famiglia.relazione", "Figlia", "figlio"},
		{"leo:famiglia.relazione", "figlio/a", "figlio"},
		{"giulia:famiglia.relazione", "moglie", "coniuge"},
		{"giulia:famiglia.relazione", "Wife", "coniuge"},
		{"giulia:famiglia.relazione", "unita civilmente", "unito_civilmente"},
		{"nonna:famiglia.relazione", "nonna", "nonno"},
		{"anna:famiglia.relazione", "madre", "genitore"},
		{"anna:famiglia.relazione", "papà", "genitore"},
		{"marco:famiglia.relazione", "nipote collaterale", "nipote_collaterale"},
		{"luca:famiglia.relazione", "nuora", "genero_nuora"},
		{"leo:famiglia.stato_famiglia", "Sì", "si"},
		{"leo:famiglia.stato_famiglia", "no", "no"},
		{"leo:famiglia.a_carico_percentuale", "50 %", "50"},
		{"leo:famiglia.a_carico_percentuale", "100", "100"},
		{"leo:famiglia.a_carico_percentuale", "0", "0"},
		{"leo:famiglia.genitori", "IO, giulia", "io,giulia"},
		{"leo:famiglia.genitori", "giulia", "giulia"},
		{"nonno:famiglia.rappresentanza", "Amministratore di sostegno", "amministratore_sostegno"},
		{"nonno:famiglia.rappresentanza", "tutore", "tutore"},
		{"famiglia.intestatario_scheda", "io", "io"},
		{"famiglia.intestatario_scheda", "si", "io"},
		{"famiglia.intestatario_scheda", "Giulia", "giulia"},
		{"leo:famiglia.note", "anything", "anything"}, // no rule
	}
	for _, tt := range tests {
		got, err := NormalizeAndValidate(tt.id, tt.in)
		if err != nil || got != tt.want {
			t.Errorf("NormalizeAndValidate(%q, %q) = %q, %v; want %q", tt.id, tt.in, got, err, tt.want)
		}
	}
}

func TestFamiglia_Rejects(t *testing.T) {
	tests := []struct{ id, in string }{
		{"leo:famiglia.relazione", "amico"},
		{"leo:famiglia.stato_famiglia", "forse"},
		{"leo:famiglia.a_carico_percentuale", "30"},
		{"leo:famiglia.genitori", ""},
		{"leo:famiglia.genitori", "io,giulia,marco"},
		{"leo:famiglia.genitori", "io,io"},
		{"leo:famiglia.genitori", "leo"},      // their own parent
		{"leo:famiglia.genitori", "Giu Lia!"}, // not a slug
		{"leo:famiglia.rappresentanza", "babysitter"},
		{"famiglia.intestatario_scheda", "io,giulia"},
		{"famiglia.intestatario_scheda", "no"},
		// Placement: owner fields without a prefix, member fields with one.
		{"famiglia.relazione", "coniuge"},
		{"famiglia.genitori", "io"},
		{"leo:famiglia.intestatario_scheda", "io"},
	}
	for _, tt := range tests {
		_, err := NormalizeAndValidate(tt.id, tt.in)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("NormalizeAndValidate(%q, %q) = %v, want *ValidationError", tt.id, tt.in, err)
		}
	}
}

func TestRelazioneANPRAndLabel(t *testing.T) {
	tests := []struct{ key, sex, anpr, label string }{
		{"coniuge", "F", "2", "moglie"},
		{"coniuge", "M", "2", "marito"},
		{"figlio", "f", "3", "figlia"},
		{"figlio", "", "3", "figlio/a"},
		{"unito_civilmente", "M", "28", "unito civilmente"},
		{"convivente", "F", "23", "convivente"},
		{"genitore", "F", "6", "madre"},
		{"genero_nuora", "F", "16", "nuora"},
		{"altro", "M", "", "altro"},
		{"sconosciuta", "M", "", ""},
	}
	for _, tt := range tests {
		if got := RelazioneANPR(tt.key); got != tt.anpr {
			t.Errorf("RelazioneANPR(%q) = %q, want %q", tt.key, got, tt.anpr)
		}
		if got := RelazioneLabel(tt.key, tt.sex); got != tt.label {
			t.Errorf("RelazioneLabel(%q, %q) = %q, want %q", tt.key, tt.sex, got, tt.label)
		}
	}
	// Every relazione has a stored key that names itself.
	for _, r := range relazioni {
		if got, err := NormalizeAndValidate("x:famiglia.relazione", r.key); err != nil || got != r.key {
			t.Errorf("relazione %q normalizes to %q, %v", r.key, got, err)
		}
	}
}

// A value naming people must name ones in the vault.
func TestFamiglia_RefsMustExist(t *testing.T) {
	v, _ := tmpVault(t)
	var verr *ValidationError
	if err := v.Set("leo:famiglia.genitori", "io,giulia", ""); !errors.As(err, &verr) || !strings.Contains(verr.Msg, "giulia") {
		t.Fatalf("genitori naming a missing member = %v", err)
	}
	if err := v.Set("giulia:famiglia.relazione", "moglie", ""); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("leo:famiglia.genitori", "io,giulia", ""); err != nil {
		t.Fatalf("genitori with giulia present: %v", err)
	}
	if err := v.Set("famiglia.intestatario_scheda", "marco", ""); !errors.As(err, &verr) {
		t.Fatalf("intestatario naming a missing member = %v", err)
	}
	if err := v.Set("pratiche.cie-leo_persona", "leo", ""); err != nil {
		t.Fatalf("pratica persona naming leo (who has a field): %v", err)
	}
	if err := v.Set("pratiche.cie-marco_persona", "marco", ""); !errors.As(err, &verr) {
		t.Fatalf("pratica persona naming a missing member = %v", err)
	}
}

func TestExpiring_Members(t *testing.T) {
	v, _ := tmpVault(t)
	now := time.Date(2026, 10, 24, 15, 0, 0, 0, time.UTC)
	must := func(id, value string) {
		t.Helper()
		if err := v.Set(id, value, ""); err != nil {
			t.Fatalf("Set(%s): %v", id, err)
		}
	}
	must("cie.data_scadenza", "2026-12-01")
	must("leo:cie.data_scadenza", "2026-11-01")
	must("pratiche.cie-leo_data_scadenza", "2026-11-15")
	must("pratiche.cie-leo_persona", "leo")
	must("pratiche.tari_data_scadenza", "2026-11-20")
	must("pratiche.tari_persona", "io")

	items, err := v.Expiring(90*24*time.Hour, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Document+"/"+it.Member)
	}
	if want := "cie/leo,pratica cie-leo/leo,pratica tari/,cie/"; strings.Join(got, ",") != want {
		t.Fatalf("expiring = %v, want %s", got, want)
	}

	// An owner-only filter never sees Leo's dates.
	owner, _ := v.Expiring(90*24*time.Hour, func(id, tier string) bool { return ScopeAllows("cie.*,pratiche.*", id) }, now)
	for _, it := range owner {
		if it.FieldID == "leo:cie.data_scadenza" {
			t.Fatalf("owner scope saw %+v", it)
		}
	}
}
