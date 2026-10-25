package vault

import (
	"errors"
	"testing"
)

// Test vectors were cross-checked against an independent implementation.
func TestValidateCodiceFiscale_Valid(t *testing.T) {
	tests := []string{
		"CCCFBA85D03L219P", // python-codicefiscale README example
		"RSSMRA85T10A562S", // common reference example
		"CCCFBA85D43L219T", // female: day + 40
		"CCCFBA85D03L21VE", // omocodia: last place digit 9 -> V
		"07643520567",      // 11-digit numeric (legal entity / temporary)
	}
	for _, cf := range tests {
		if err := ValidateCodiceFiscale(cf); err != nil {
			t.Errorf("ValidateCodiceFiscale(%q) = %v, want nil", cf, err)
		}
	}
}

func TestValidateCodiceFiscale_Invalid(t *testing.T) {
	tests := []struct {
		cf     string
		reason string
	}{
		{"", "empty"},
		{"CCCFBA85D03L219", "too short"},
		{"CCCFBA85D03L219PX", "too long"},
		{"CCCFBA85D03L219A", "wrong check character"},
		{"CCC1BA85D03L219P", "digit in name part"},
		{"CCCFBA85Z03L219P", "invalid month letter"},
		{"CCCFBA85D32L219P", "day 32"},
		{"CCCFBA85D00L219P", "day 00"},
		{"CCCFBA85D72L219P", "female day 72"},
		{"CCCFBA85D03L21WP", "W is not an omocodia letter"},
		{"CCCFBA85D031219P", "place code must start with a letter"},
		{"01234567890", "11-digit with bad check digit"},
	}
	for _, tt := range tests {
		if err := ValidateCodiceFiscale(tt.cf); err == nil {
			t.Errorf("ValidateCodiceFiscale(%q) = nil, want error (%s)", tt.cf, tt.reason)
		}
	}
}

func TestValidatePartitaIVA(t *testing.T) {
	for _, p := range []string{"00743110157", "07643520567", "12345678903"} {
		if err := ValidatePartitaIVA(p); err != nil {
			t.Errorf("ValidatePartitaIVA(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range []string{"", "0074311015", "007431101570", "0074311015A", "01234567890", "00743110158"} {
		if err := ValidatePartitaIVA(p); err == nil {
			t.Errorf("ValidatePartitaIVA(%q) = nil, want error", p)
		}
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	tests := []struct {
		id, in, want string
	}{
		{"identity.codice_fiscale", " cccfba85d03l219p ", "CCCFBA85D03L219P"},
		{"identity.codice_fiscale", "CCC FBA 85D03 L219P", "CCCFBA85D03L219P"},
		{"nucleo_familiare.membro1_codice_fiscale", "rssmra85t10a562s", "RSSMRA85T10A562S"},
		{"business.codice_fiscale", "07643520567", "07643520567"},
		{"business.partita_iva", "IT 00743110157", "00743110157"},
		{"cie.numero", "ca12345ab", "CA12345AB"},
		{"residenza.cap", " 80121 ", "80121"},
		{"residenza.provincia", "na", "NA"},
		{"domicilio.provincia", "mi", "MI"},
		{"identity.birth_province", "ee", "EE"},
		{"residenza.interno", "13", "13"},
		{"residenza.interno", " int. 13 ", "13"},
		{"domicilio.interno", "Interno 4B", "4B"},
		{"residenza.comune", "Napoli", "Napoli"},       // no rule: unchanged
		{"identity.full_name", "  Jane  ", "  Jane  "}, // no rule: unchanged
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

func TestNormalizeAndValidate_Rejects(t *testing.T) {
	tests := []struct{ id, in string }{
		{"identity.codice_fiscale", "CCCFBA85D03L219A"},
		{"nucleo_familiare.membro2_codice_fiscale", "not-a-code"},
		{"business.partita_iva", "00743110158"},
		{"cie.numero", "AA1234567"}, // old paper ID format
		{"residenza.cap", "8012"},
		{"residenza.provincia", "Napoli"},
		{"residenza.interno", "int."},
		{"domicilio.interno", "the one at the end of the hall"},
	}
	for _, tt := range tests {
		_, err := NormalizeAndValidate(tt.id, tt.in)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("NormalizeAndValidate(%q, %q) = %v, want *ValidationError", tt.id, tt.in, err)
		}
	}
}

func TestIsCredentialField(t *testing.T) {
	forbidden := []string{"spid.password", "spid.username", "spid.otp", "SPID.Password", "cie.pin", "cie.puk", "cie.pin_1", "cie.second_pin",
		"cie.pin1", "cie.puk2", "cieid.pin", "identity.spid_password", "documents.cie_pin", "credentials.spid-otp", "Identity.SPID_Password",
		"spid.login", "cieid.username", "spid.pass", "spid.pw", "spid.recovery_codes", "cieid.codice", "identity.spid_secret",
		"identity.spidPassword", "identity.spidpassword", "identity.ciePin1", "notes.pinspid"}
	for _, id := range forbidden {
		if !IsCredentialField(id) {
			t.Errorf("IsCredentialField(%q) = false, want true", id)
		}
	}
	allowed := []string{"cie.numero", "cie.data_scadenza", "identity.codice_fiscale", "identity.pinterest", "custom.spid",
		"cie.pinterest", "documents.cie_scan", "identity.password_hint", "accounts.bank_pin", "spid.provider", "spid.data_scadenza",
		"identity.passaporto", "notes.specie", "identity.cieloazzurro"}
	for _, id := range allowed {
		if IsCredentialField(id) {
			t.Errorf("IsCredentialField(%q) = true, want false", id)
		}
	}
}

func TestDefaultSensitivity_Italian(t *testing.T) {
	tests := []struct{ id, want string }{
		{"identity.codice_fiscale", "sensitive"},
		{"cie.numero", "sensitive"},
		{"residenza.indirizzo", "sensitive"},
		{"residenza.comune", "standard"},
		{"nucleo_familiare.membro1_codice_fiscale", "sensitive"},
		{"nucleo_familiare.membro1_nome", "standard"},
		// Validation ignores case, so defaults do too.
		{"financial.SSN", "critical"},
		{"Payment.card_number", "critical"},
		{"Identity.date_of_birth", "sensitive"},
	}
	for _, tt := range tests {
		if got := DefaultSensitivity(tt.id); got != tt.want {
			t.Errorf("DefaultSensitivity(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestSuggestCanonical_Italian(t *testing.T) {
	tests := []struct{ id, want string }{
		{"identity.cf", "identity.codice_fiscale"},
		{"identity.tax_code", "identity.codice_fiscale"},
		{"business.piva", "business.partita_iva"},
		{"business.vat_number", "business.partita_iva"},
		{"cie.scadenza", "cie.data_scadenza"},
		{"residenza.via", "residenza.indirizzo"},
		{"residenza.numero_interno", "residenza.interno"},
		{"domicilio.int", "domicilio.interno"},
	}
	for _, tt := range tests {
		s := SuggestCanonical(tt.id)
		if s == nil || s.Canonical != tt.want {
			t.Errorf("SuggestCanonical(%q) = %+v, want %q", tt.id, s, tt.want)
		}
	}
}

func TestVaultSet_ItalianValidation(t *testing.T) {
	v, _ := tmpVault(t)

	if err := v.Set("identity.codice_fiscale", "cccfba85d03l219p", "sensitive"); err != nil {
		t.Fatalf("Set valid codice fiscale: %v", err)
	}
	f, err := v.Get("identity.codice_fiscale")
	if err != nil {
		t.Fatal(err)
	}
	if f.Value != "CCCFBA85D03L219P" {
		t.Errorf("stored value = %q, want normalized CCCFBA85D03L219P", f.Value)
	}

	var verr *ValidationError
	if err := v.Set("identity.codice_fiscale", "CCCFBA85D03L219A", ""); !errors.As(err, &verr) {
		t.Errorf("Set invalid codice fiscale: got %v, want *ValidationError", err)
	}
	// The invalid write must not overwrite the stored value.
	f, _ = v.Get("identity.codice_fiscale")
	if f.Value != "CCCFBA85D03L219P" {
		t.Errorf("value after rejected write = %q", f.Value)
	}

	// Credentials are stored only as personal.
	for _, id := range []string{"spid.password", "cie.pin", "identity.spid_password"} {
		if err := v.Set(id, "12345678", ""); err != nil {
			t.Errorf("Set %s: %v", id, err)
		}
		if f, _ := v.Get(id); f == nil || f.Sensitivity != TierPersonal {
			t.Errorf("%s stored as %+v, want personal", id, f)
		}
		if err := v.Set(id, "12345678", "critical"); !errors.As(err, &verr) {
			t.Errorf("Set %s as critical: got %v, want ValidationError", id, err)
		}
		if err := v.SetSensitivity(id, "standard"); !errors.As(err, &verr) {
			t.Errorf("lowering %s: got %v, want ValidationError", id, err)
		}
	}
	// Non-secret SPID details default to personal but may be lowered.
	v.Set("spid.provider", "PosteID", "")
	if f, _ := v.Get("spid.provider"); f.Sensitivity != TierPersonal {
		t.Errorf("spid.provider default = %s, want personal", f.Sensitivity)
	}
	if err := v.SetSensitivity("spid.provider", "standard"); err != nil {
		t.Errorf("lowering spid.provider: %v", err)
	}
}

func TestNormalizeAndValidate_CaseInsensitiveRules(t *testing.T) {
	if _, err := NormalizeAndValidate("Identity.Codice_Fiscale", "garbage"); err == nil {
		t.Error("mixed-case codice_fiscale field should still be validated")
	}
	got, err := NormalizeAndValidate("CIE.numero", "ca12345ab")
	if err != nil || got != "CA12345AB" {
		t.Errorf("CIE.numero: got %q, %v", got, err)
	}
	if got := DefaultSensitivity("Nucleo_Familiare.MEMBRO1_CODICE_FISCALE"); got != "sensitive" {
		t.Errorf("mixed-case member codice fiscale sensitivity = %q", got)
	}
}

// Vectors cross-checked with an independent mod-97 implementation.
func TestIBAN(t *testing.T) {
	ok := map[string]string{
		"IT60 X054 2811 1010 0000 0123 456": "IT60X0542811101000000123456",
		"it60x0542811101000000123456":       "IT60X0542811101000000123456",
		"GB82 WEST 1234 5698 7654 32":       "GB82WEST12345698765432",
	}
	for in, want := range ok {
		got, err := NormalizeAndValidate("financial.iban", in)
		if err != nil || got != want {
			t.Errorf("IBAN %q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"IT60X0542811101000000123457", // wrong check digits
		"IT60X054281110100000012345",  // Italian IBAN too short
		"60X0542811101000000123456",   // no country code
		"IT60-X054",                   // too short
	} {
		if _, err := NormalizeAndValidate("financial.iban", in); err == nil {
			t.Errorf("IBAN %q should be rejected", in)
		}
	}
	if got := DefaultSensitivity("financial.iban"); got != "sensitive" {
		t.Errorf("iban sensitivity = %q", got)
	}
	for id, want := range map[string]string{"identity.cellulare": "identity.phone", "financial.conto_corrente": "financial.iban"} {
		if s := SuggestCanonical(id); s == nil || s.Canonical != want {
			t.Errorf("SuggestCanonical(%q) = %+v, want %s", id, s, want)
		}
	}
}

func TestCIEPinPuk(t *testing.T) {
	if got, err := NormalizeAndValidate("cie.pin", "1234 5678"); err != nil || got != "12345678" {
		t.Errorf("cie.pin: %q %v", got, err)
	}
	for _, in := range []string{"1234", "123456789", "abcd5678"} {
		if _, err := NormalizeAndValidate("cie.puk", in); err == nil {
			t.Errorf("cie.puk %q should be rejected", in)
		}
	}
}
