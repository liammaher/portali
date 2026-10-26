package vault

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// tmpVaultKind is tmpVault for a vault of the given kind.
func tmpVaultKind(t *testing.T, kind string) *Vault {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".pvault")
	sk, err := InitWithOptions(dir, testPassword, InitOptions{Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	v, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	if _, err := v.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestKind_StoredAndReported(t *testing.T) {
	for _, kind := range []string{KindPersona, KindImpresa} {
		v := tmpVaultKind(t, kind)
		if v.Kind() != kind {
			t.Errorf("Kind() = %q, want %q", v.Kind(), kind)
		}
		st, err := v.Status()
		if err != nil || st.Kind != kind {
			t.Errorf("Status().Kind = %q, %v; want %q", st.Kind, err, kind)
		}
	}
	// A vault from before kinds (no meta) is persona.
	v, _ := tmpVault(t)
	if v.Kind() != KindPersona {
		t.Errorf("default kind = %q", v.Kind())
	}
	if _, err := InitWithOptions(filepath.Join(t.TempDir(), "x"), testPassword, InitOptions{Kind: "azienda"}); err == nil {
		t.Error("an unknown kind was accepted")
	}
}

func TestKind_FieldRules(t *testing.T) {
	persona := tmpVaultKind(t, KindPersona)
	impresa := tmpVaultKind(t, KindImpresa)
	// People must exist before a field names them; ruolo.tipo creates one.
	if err := impresa.Set("bianchi:ruolo.tipo", "titolare", ""); err != nil {
		t.Fatal(err)
	}
	if err := persona.Set("leo:famiglia.relazione", "figlio", ""); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		v        *Vault
		id, val  string
		wantFail bool
	}{
		// The business's own data is the business's.
		{impresa, "business.denominazione", "Studio Bianchi", false},
		{impresa, "business.forma_giuridica", "S.r.l.s.", false},
		{impresa, "locale.comune", "Napoli", false},
		{impresa, "pratiche.cila-rossi_stato", "da_fare", false},
		{impresa, "preferences.timezone", "Europe/Rome", false},
		{impresa, "financial.iban", "IT60X0542811101000000123456", false},
		{impresa, "custom.anything", "x", false}, // user-defined categories anywhere
		{impresa, "identity.codice_fiscale", "RSSMRA85T10A562S", true},
		{impresa, "residenza.comune", "Napoli", true},
		{impresa, "abitazione.titolo", "locatario", true},
		{impresa, "albo.numero", "1234", true},
		{impresa, "famiglia.intestatario_scheda", "io", true},
		{impresa, "business.qualita_dichiarante", "titolare", true},
		// People in a business vault: persons, companies, a ruolo; never
		// credentials or SPID/CieID accounts.
		{impresa, "bianchi:identity.codice_fiscale", "RSSMRA85T10A562S", false},
		{impresa, "bianchi:albo.numero", "1234", false},
		{impresa, "bianchi:firma_digitale.data_scadenza", "2027-01-31", false},
		{impresa, "bianchi:cie.numero", "CA12345AB", false},
		{impresa, "bianchi:business.denominazione", "Bianchi Costruzioni SRL", false},
		{impresa, "bianchi:famiglia.relazione", "figlio", true},
		{impresa, "bianchi:spid.provider", "posteid", true},
		{impresa, "bianchi:cie.pin", "12345678", true},
		{impresa, "bianchi:firma_digitale.pin", "12345678", true},
		{impresa, "bianchi:identity.spid_password", "x", true},
		// Numbered copies of a person's categories, and secrets that are
		// always a person's (CIE, CieID, digital signature), are refused as
		// the business's own data; its own SPID is allowed.
		{impresa, "identity2.codice_fiscale", "RSSMRA85T10A562S", true},
		{impresa, "cie2.numero", "CA12345AB", true},
		{impresa, "identity-2.first_name", "Anna", true},
		{impresa, "cieid.username", "x", true},
		{impresa, "custom.cie_pin", "12345678", true},
		{impresa, "firma.pin", "12345678", true},
		{impresa, "cns.puk", "12345678", true},
		{impresa, "spid.provider", "posteid", false},
		{impresa, "spid2.livello", "2", false},
		{impresa, "bianchi:firmadigitale.pin", "12345678", true},
		{impresa, "bianchi:smartcard.pin", "12345678", true},
		{impresa, "bianchi:cie.codice_sblocco", "12345678", true},
		// Any login secret is a credential: a person's portal logins are
		// theirs, while the business's own (Entratel, Fisconline) are allowed.
		{impresa, "bianchi:fisconline.pin", "1234567890", true},
		{impresa, "bianchi:pec.password", "x", true},
		{impresa, "bianchi:inps.pin", "x", true},
		{impresa, "entratel.password", "x", false},
		{impresa, "fisconline.pin", "1234567890", false},
		// Personal vaults: as before, plus no ruolo.
		{persona, "identity.codice_fiscale", "RSSMRA85T10A562S", false},
		{persona, "business.qualita_dichiarante", "titolare", false},
		{persona, "business.forma_giuridica", "ditta", false},
		{persona, "albo.numero", "1234", false},
		{persona, "leo:famiglia.genitori", "io", false},
		{persona, "leo:ruolo.tipo", "dipendente", true},
	}
	for _, tt := range tests {
		tier := ""
		if IsCredentialField(tt.id) || IsPersonalCategory(strings.SplitN(tt.id, ".", 2)[0]) {
			tier = TierPersonal // only the kind rules may refuse these, not the tier rule
		}
		err := tt.v.Set(tt.id, tt.val, tier)
		var verr *ValidationError
		switch {
		case tt.wantFail && !errors.As(err, &verr):
			t.Errorf("%s vault: Set(%q) = %v, want *ValidationError", tt.v.Kind(), tt.id, err)
		case !tt.wantFail && err != nil:
			t.Errorf("%s vault: Set(%q) = %v, want nil", tt.v.Kind(), tt.id, err)
		}
	}
	// A person's credential is refused in a business vault even as personal.
	if err := impresa.Set("bianchi:cie.pin", "12345678", TierPersonal); err == nil {
		t.Error("business vault stored a person's CIE PIN")
	}
	// The personal vault keeps a family member's PIN at the personal tier.
	if err := persona.Set("leo:cie.pin", "12345678", TierPersonal); err != nil {
		t.Errorf("personal vault refused a family member's CIE PIN: %v", err)
	}
}

func TestKind_DocumentRules(t *testing.T) {
	impresa := tmpVaultKind(t, KindImpresa)
	persona := tmpVaultKind(t, KindPersona)
	data := []byte("%PDF-1.4 test")
	tests := []struct {
		v        *Vault
		category string
		wantFail bool
	}{
		{impresa, "business", false},
		{impresa, "locale", false},
		{impresa, "bianchi:cie", false},
		{impresa, "bianchi:procura", false},
		{impresa, "cie", true},
		{impresa, "famiglia", true},
		{impresa, "bianchi:spid", true},
		{impresa, "bianchi:cieid_poste", true},
		{impresa, "cieid", true},
		{impresa, "cie2", true},
		{impresa, "spid", false},
		{impresa, "fisconline_pin", false},
		{impresa, "bianchi:fisconline_pin", true},
		{impresa, "firma_pin", true},
		{persona, "cie", false},
		{persona, "leo:spid", false},
		{persona, "ruolo", true},
	}
	for _, tt := range tests {
		_, err := tt.v.AddDocument(tt.category, "scan.pdf", "application/pdf", data, "")
		var verr *ValidationError
		switch {
		case tt.wantFail && !errors.As(err, &verr):
			t.Errorf("%s vault: AddDocument(%q) = %v, want *ValidationError", tt.v.Kind(), tt.category, err)
		case !tt.wantFail && err != nil:
			t.Errorf("%s vault: AddDocument(%q) = %v, want nil", tt.v.Kind(), tt.category, err)
		}
	}
}

func TestKind_MigrateNucleoRefused(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	if _, err := v.MigrateNucleo(true); err == nil {
		t.Error("MigrateNucleo ran in a business vault")
	}
}

func TestKind_PersonRefMessageNamesRuolo(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	err := v.Set("pratiche.cila-rossi_persona", "rossi", "")
	if err == nil || !strings.Contains(err.Error(), "--ruolo") {
		t.Errorf("Set naming a missing person = %v, want a hint with --ruolo", err)
	}
}

func TestKind_HouseholdIgnoresCategoryCase(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	setLegacy(t, v, "bianchi:Ruolo.tipo", "socio")
	h, err := v.Household(nil, time.Now())
	if err != nil || len(h.Members) != 1 || h.Members[0].Ruolo["tipo"] != "socio" {
		t.Fatalf("household = %+v, %v", h, err)
	}
}

func TestKind_HouseholdShowsRuolo(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	for id, val := range map[string]string{
		"bianchi:ruolo.tipo":              "titolare, legale rappresentante",
		"bianchi:identity.first_name":     "Anna",
		"edil-srl:ruolo.tipo":             "impresa esecutrice",
		"edil-srl:business.denominazione": "Edil SRL",
	} {
		if err := v.Set(id, val, ""); err != nil {
			t.Fatalf("Set(%q): %v", id, err)
		}
	}
	h, err := v.Household(nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]MemberInfo{}
	for _, m := range h.Members {
		got[m.Slug] = m
	}
	if r := got["bianchi"].Ruolo["tipo"]; r != "titolare,legale_rappresentante" {
		t.Errorf("bianchi ruolo.tipo = %q", r)
	}
	if got["bianchi"].FirstName != "Anna" {
		t.Errorf("bianchi first name = %q", got["bianchi"].FirstName)
	}
	if got["edil-srl"].Denominazione != "Edil SRL" || got["edil-srl"].Ruolo["tipo"] != "impresa_esecutrice" {
		t.Errorf("edil-srl = %+v", got["edil-srl"])
	}
}

func TestRuolo_Normalize(t *testing.T) {
	good := map[string]string{
		"socio":                           "socio",
		"Socia, Amministratrice":          "socio,amministratore",
		"legale rappresentante; titolare": "legale_rappresentante,titolare",
		"committente":                     "cliente",
		"dipendente,dipendente":           "dipendente",
		"Impresa esecutrice":              "impresa_esecutrice",
	}
	for in, want := range good {
		if got, err := NormalizeAndValidate("x:ruolo.tipo", in); err != nil || got != want {
			t.Errorf("ruolo.tipo %q = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "amico", "socio, amico", " , "} {
		if _, err := NormalizeAndValidate("x:ruolo.tipo", in); err == nil {
			t.Errorf("ruolo.tipo %q accepted", in)
		}
	}
	for in, want := range map[string]string{"50": "50", "33,33": "33.33", "100%": "100", "33.333": "33.333", "33,333 %": "33.333", "0.125": "0.125"} {
		if got, err := NormalizeAndValidate("x:ruolo.quota_percentuale", in); err != nil || got != want {
			t.Errorf("quota %q = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"0", "101", "-5", "metà"} {
		if _, err := NormalizeAndValidate("x:ruolo.quota_percentuale", in); err == nil {
			t.Errorf("quota %q accepted", in)
		}
	}
	if _, err := NormalizeAndValidate("ruolo.tipo", "socio"); err == nil {
		t.Error("ruolo.tipo without a person prefix accepted")
	}
	if _, err := NormalizeAndValidate("x:ruolo.data_fine", "31/02/2026"); err == nil {
		t.Error("an impossible ruolo.data_fine was accepted")
	}
	// Every role named in the schema description normalizes to itself.
	for _, r := range ruoli {
		if got, err := NormalizeAndValidate("x:ruolo.tipo", r); err != nil || got != r {
			t.Errorf("ruolo %q normalizes to %q, %v", r, got, err)
		}
		if !strings.Contains(GetSchemaField("ruolo.tipo").Description, r) {
			t.Errorf("schema description lacks %q", r)
		}
	}
}

func TestBusiness_NewFields(t *testing.T) {
	good := map[[2]string]string{
		{"business.forma_giuridica", "S.r.l."}:                "srl",
		{"business.forma_giuridica", "SRL semplificata"}:      "srls",
		{"business.forma_giuridica", "Ditta individuale"}:     "ditta_individuale",
		{"business.forma_giuridica", "società cooperativa"}:   "cooperativa",
		{"business.forma_giuridica", "libero professionista"}: "lavoratore_autonomo",
		{"business.cciaa", " na "}:                            "NA",
		{"business.capitale_sociale", "10.000"}:               "10000.00",
		{"business.data_cessazione", "31/12/2026"}:            "2026-12-31",
		{"business.durc_data_scadenza", "2027-02-01"}:         "2027-02-01",
	}
	for k, want := range good {
		if got, err := NormalizeAndValidate(k[0], k[1]); err != nil || got != want {
			t.Errorf("%s %q = %q, %v; want %q", k[0], k[1], got, err, want)
		}
	}
	for _, k := range [][2]string{
		{"business.forma_giuridica", "multinazionale"},
		{"business.cciaa", "Napoli"},
		{"business.capitale_sociale", "tanti"},
	} {
		if _, err := NormalizeAndValidate(k[0], k[1]); err == nil {
			t.Errorf("%s %q accepted", k[0], k[1])
		}
	}
	for _, f := range formeGiuridiche {
		if !strings.Contains(GetSchemaField("business.forma_giuridica").Description, f) {
			t.Errorf("forma_giuridica description lacks %q", f)
		}
	}
}

func TestFirmaDigitaleCredentials(t *testing.T) {
	for _, id := range []string{"firma_digitale.pin", "firma_digitale.puk", "x:firma_digitale.pin", "cns.pin"} {
		if !IsCredentialField(id) {
			t.Errorf("IsCredentialField(%q) = false", id)
		}
	}
	for _, id := range []string{"firma_digitale.data_scadenza", "firma_digitale.ente_emittente"} {
		if IsCredentialField(id) {
			t.Errorf("IsCredentialField(%q) = true", id)
		}
	}
}

func TestSchema_SubjectAndKinds(t *testing.T) {
	kinds := []string{KindPersona, KindImpresa}
	for _, c := range RecommendedSchema.Categories {
		if c.Subject != "" && c.Subject != SubjectPersona && c.Subject != SubjectImpresa {
			t.Errorf("category %s: subject %q", c.Name, c.Subject)
		}
		for _, k := range c.Kinds {
			if !slices.Contains(kinds, k) {
				t.Errorf("category %s: kind %q", c.Name, k)
			}
		}
		for _, f := range c.Fields {
			for _, k := range f.Kinds {
				if !slices.Contains(kinds, k) {
					t.Errorf("field %s: kind %q", f.ID, k)
				}
			}
		}
		// The schema's scope and for values stay the ones pratiche-napoli's
		// gen-fields accepts.
		if c.Scope != ScopePerson && c.Scope != ScopeOwner {
			t.Errorf("category %s: scope %q", c.Name, c.Scope)
		}
		for _, f := range c.Fields {
			if f.For != "" && f.For != ForOwner && f.For != ForPerson {
				t.Errorf("field %s: for %q", f.ID, f.For)
			}
		}
	}
	if GetSchemaCategory("ruolo") == nil || GetSchemaCategory("nope") != nil {
		t.Error("GetSchemaCategory")
	}
}
