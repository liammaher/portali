package vault

import (
	"errors"
	"testing"
)

func TestNormalizeAndValidate_Access(t *testing.T) {
	tests := []struct {
		id, in, want string
	}{
		{"preferences.accesso_pa", " SPID ", "spid"},
		{"preferences.accesso_pa", "cie", "cie"},
		{"preferences.accesso_pa", "CieID", "cie"},
		{"spid.provider", "PosteID", "PosteID"},
		{"spid.provider", "poste id", "PosteID"},
		{"spid.provider", "Poste Italiane", "PosteID"},
		{"spid.provider", "aruba", "Aruba"},
		{"spid.provider", "InfoCert ID", "InfoCert"},
		{"spid.provider", "register.it", "Register"},
		{"spid.provider", "SPIDItalia", "Register"},
		{"spid.provider", "TIM id", "TIM id"},
		{"spid.provider", "TI Trust Technologies", "TIM id"},
		{"spid.provider", "intesi", "Intesi Group"},
		{"spid.provider", "Etna ID", "EtnaID"},
		{"spid.provider", "teamsystem", "TeamSystem"},
		{"spid.provider", "InfoCamere", "InfoCamere"},
		// Canonical spellings are stable.
		{"spid.provider", "Intesi Group", "Intesi Group"},
		{"spid.provider", "Namirial", "Namirial"},
		{"spid.provider", "Sielte", "Sielte"},
		{"spid.provider", "Lepida", "Lepida"},
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

func TestNormalizeAndValidate_AccessInvalid(t *testing.T) {
	for _, tt := range []struct{ id, in string }{
		{"preferences.accesso_pa", "cns"},
		{"preferences.accesso_pa", ""},
		{"preferences.accesso_pa", "both"},
		{"spid.provider", "Google"},
		{"spid.provider", ""},
		{"spid.provider", "Poste Mobile"},
	} {
		_, err := NormalizeAndValidate(tt.id, tt.in)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("NormalizeAndValidate(%q, %q) = %v, want ValidationError", tt.id, tt.in, err)
		}
	}
}

func TestAccessPreferenceIsReadable(t *testing.T) {
	v, _ := tmpVault(t)
	if err := v.Set("preferences.accesso_pa", "SPID", ""); err != nil {
		t.Fatal(err)
	}
	f, err := v.Get("preferences.accesso_pa")
	if err != nil {
		t.Fatal(err)
	}
	if f.Value != "spid" || f.Sensitivity != "standard" {
		t.Errorf("preferences.accesso_pa = %q (%s), want spid (standard)", f.Value, f.Sensitivity)
	}

	// The provider is validated in its personal default tier too, and stays
	// lowerable so an agent can read it.
	if err := v.Set("spid.provider", "Google", ""); err == nil {
		t.Error("spid.provider accepted an unknown provider")
	}
	if err := v.Set("spid.provider", "poste", ""); err != nil {
		t.Fatal(err)
	}
	if err := v.SetSensitivity("spid.provider", "standard"); err != nil {
		t.Fatal(err)
	}
	if f, _ := v.Get("spid.provider"); f.Value != "PosteID" || f.Sensitivity != "standard" {
		t.Errorf("spid.provider = %q (%s), want PosteID (standard)", f.Value, f.Sensitivity)
	}
}
