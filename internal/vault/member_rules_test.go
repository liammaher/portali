package vault

import (
	"errors"
	"strings"
	"testing"
)

// memberRuleIDs are the IDs whose rules a member must share with the owner:
// every schema field, the credential tables and the provider-named accounts.
func memberRuleIDs() []string {
	var ids []string
	for _, c := range RecommendedSchema.Categories {
		for _, f := range c.Fields {
			ids = append(ids, f.ID)
		}
	}
	ids = append(ids, credentialIDs...)
	ids = append(ids, nonCredentialIDs...)
	return append(ids,
		"spid_poste.password", "spid-aruba.password", "cie_id.pin", "identity.spid_password2",
		"identity.spid_passwords", "cie.pincode", "identity.cie_pukcode", "spid_poste.provider",
		"spid2.password", "cieid2.pin", "Payment.card_number", "financial.SSN", "Identity.date_of_birth",
		"business.codice_fiscale", "custom.my_codice_fiscale", "custom.mio_iban", "custom.data_x")
}

// memberProbeValues exercise every format rule.
var memberProbeValues = []string{
	"", "x", "ca12345ab", "CA12345AB", "12345678", "1234 5678", " 80121 ", "8012", "na", "Napoli",
	" int. 13 ", "2026-01-31", "31/01/2026", "31/02/2026", "rssmra85t10a562s", "RSSMRA85T10A562A",
	"IT60X0542811101000000123456", "IT 00743110157", "00743110158", "spid", "Poste ID", "A/2",
}

// A member's field must get exactly the owner's tier defaults, credential
// treatment and format rules: a rule that missed the prefix would store
// leo:spid.password or leo:cie.pin where an agent can read it.
func TestMemberRulesMatchOwner(t *testing.T) {
	for _, prefix := range []string{"x:", "X:", "leo:"} {
		for _, id := range memberRuleIDs() {
			pid := prefix + id
			category := strings.SplitN(id, ".", 2)[0]
			if IsOwnerOnlyCategory(category) {
				var verr *ValidationError
				if _, err := NormalizeAndValidate(pid, "x"); !errors.As(err, &verr) {
					t.Errorf("NormalizeAndValidate(%q) = %v, want an owner-only *ValidationError", pid, err)
				}
				continue
			}
			if got, want := IsCredentialField(pid), IsCredentialField(id); got != want {
				t.Errorf("IsCredentialField(%q) = %v, %q gives %v", pid, got, id, want)
			}
			if got, want := DefaultSensitivity(pid), DefaultSensitivity(id); got != want {
				t.Errorf("DefaultSensitivity(%q) = %q, %q gives %q", pid, got, id, want)
			}
			if got, want := IsPersonalCategory(prefix+category), IsPersonalCategory(category); got != want {
				t.Errorf("IsPersonalCategory(%q) = %v, %q gives %v", prefix+category, got, category, want)
			}
			if got, want := DefaultDocumentTier(prefix+category), DefaultDocumentTier(category); got != want {
				t.Errorf("DefaultDocumentTier(%q) = %q, %q gives %q", prefix+category, got, category, want)
			}
			if f := GetSchemaField(strings.ToLower(id)); f != nil && f.For != "" {
				continue // owner and person fields differ on purpose (TestFamiglia*)
			}
			for _, value := range memberProbeValues {
				got, gotErr := NormalizeAndValidate(pid, value)
				want, wantErr := NormalizeAndValidate(id, value)
				if got != want || errMsg(gotErr) != errMsg(wantErr) {
					t.Errorf("NormalizeAndValidate(%q, %q) = %q, %v; %q gives %q, %v", pid, value, got, gotErr, id, want, wantErr)
				}
			}
		}
	}
}

func TestMemberCredentialsArePersonal(t *testing.T) {
	for _, id := range []string{"leo:spid.password", "leo:cie.pin", "leo:cie.puk", "Leo:spid.password", "leo:spid_poste.username", "leo:cieid.pin"} {
		if !IsCredentialField(id) || DefaultSensitivity(id) != TierPersonal {
			t.Errorf("%s: credential = %v, default tier %q", id, IsCredentialField(id), DefaultSensitivity(id))
		}
	}
	if got := DefaultSensitivity("leo:payment.card_number"); got != "critical" {
		t.Errorf("leo:payment.card_number defaults to %q, want critical", got)
	}
	if got := DefaultDocumentTier("leo:spid"); got != TierPersonal {
		t.Errorf("leo:spid documents default to %q, want personal", got)
	}

	v, _ := tmpVault(t)
	var verr *ValidationError
	if err := v.Set("leo:spid.password", "hunter2-secret", "standard"); !errors.As(err, &verr) {
		t.Errorf("leo:spid.password stored below personal: %v", err)
	}
	if err := v.Set("leo:cie.pin", "12345678", ""); err != nil {
		t.Fatal(err)
	}
	if f, _ := v.FieldMeta("leo:cie.pin"); f == nil || f.Sensitivity != TierPersonal {
		t.Errorf("leo:cie.pin stored as %+v, want personal", f)
	}
	if _, err := v.AddDocument("leo:spid", "codes.txt", "text/plain", []byte("x"), "sensitive"); !errors.As(err, &verr) {
		t.Errorf("leo:spid document stored below personal: %v", err)
	}
}

func TestOwnerOnlyCategories(t *testing.T) {
	v, _ := tmpVault(t)
	var verr *ValidationError
	for _, id := range []string{"leo:abitazione.titolo", "leo:Locazione.canone_mensile", "leo:tari.protocollo", "leo:pratiche.cie-leo_stato", "leo:utenze.luce_pod"} {
		if err := v.Set(id, "x", ""); !errors.As(err, &verr) {
			t.Errorf("Set(%q) = %v, want *ValidationError", id, err)
		}
	}
	if _, err := v.AddDocument("leo:abitazione", "visura.pdf", "application/pdf", []byte("%PDF"), ""); !errors.As(err, &verr) {
		t.Errorf("AddDocument(leo:abitazione) = %v, want *ValidationError", err)
	}
	if err := v.Set("leo:cie.numero", "ca12345ab", ""); err != nil {
		t.Fatal(err)
	}
	f, err := v.Get("leo:cie.numero")
	if err != nil || f.Value != "CA12345AB" || f.Category != "leo:cie" || f.Sensitivity != "sensitive" {
		t.Errorf("leo:cie.numero = %+v, %v", f, err)
	}
	// The owner's field is a different field, under a different key.
	if f, _ := v.Get("cie.numero"); f != nil {
		t.Errorf("owner's cie.numero = %+v, want none", f)
	}
}

func TestSuggestCanonical_Member(t *testing.T) {
	if s := SuggestCanonical("leo:identity.cf"); s == nil || s.Canonical != "leo:identity.codice_fiscale" {
		t.Errorf("SuggestCanonical(leo:identity.cf) = %+v", s)
	}
	if s := SuggestCanonical("leo:identity.codice_fiscale"); s != nil {
		t.Errorf("canonical member field got a suggestion: %+v", s)
	}
}
