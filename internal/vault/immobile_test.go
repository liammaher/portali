package vault

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestNormalizeAndValidate_Immobile(t *testing.T) {
	tests := []struct {
		id, in, want string
	}{
		{"immobile.cap", " 80134 ", "80134"},
		{"immobile.provincia", " na ", "NA"},
		{"immobile.interno", "int. 7", "7"},
		{"immobile.catasto_categoria", "a2", "A/2"},
		{"immobile.catasto_categoria", "C/06", "C/6"},
		{"immobile.catasto_foglio", " 112 ", "112"},
		{"immobile.catasto_particella", "34b", "34B"},
		{"immobile.catasto_subalterno", "12", "12"},
		{"immobile.superficie_mq", "85,5 mq", "85.5"},
		{"immobile.superficie_mq", "1.200", "1200"},
		{"immobile.titolo", " Proprietaria ", "proprietario"},
		{"immobile.titolo", "Co-proprietario", "comproprietario"},
		{"immobile.titolo", "comproprietaria", "comproprietario"},
		{"immobile.titolo", "conduttore", "locatario"},
		{"immobile.titolo", "usufruttuaria", "usufruttuario"},
		{"immobile.titolo", "comodataria", "comodatario"},
		{"immobile.titolo", "altro", "altro"},
		{"immobile.municipalita", "V", "5"},
		{"immobile.quartiere", "vomero", "Vomero"},
		// A client's property, and a second one, follow the same rules.
		{"luca:immobile.catasto_categoria", "a 3", "A/3"},
		{"luca:immobile2.cap", "80127", "80127"},
		{"immobile-2.titolo", "owner", "proprietario"},
		// No rule: unchanged.
		{"immobile.scala", "B", "B"},
		{"immobile.piano", "rialzato", "rialzato"},
		{"immobile.catasto_sezione", "SFE", "SFE"},
		{"immobile.destinazione_uso", "residenziale", "residenziale"},
		{"immobile.indirizzo", "Via Alabardieri", "Via Alabardieri"},
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

func TestNormalizeAndValidate_ImmobileRejects(t *testing.T) {
	tests := []struct{ id, in string }{
		{"immobile.cap", "8013"},
		{"immobile.provincia", "Napoli"},
		{"immobile.interno", "int."},
		{"immobile.catasto_categoria", "C/8"},
		{"immobile.catasto_categoria", "Z/1"},
		{"immobile.catasto_foglio", "12a"},
		{"immobile.catasto_subalterno", "sub"},
		{"immobile.superficie_mq", "0"},
		{"immobile.titolo", "ospite"}, // a home's title, not a property's
		{"immobile.municipalita", "11"},
		{"luca:immobile2.catasto_foglio", "x"},
	}
	for _, tt := range tests {
		_, err := NormalizeAndValidate(tt.id, tt.in)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("NormalizeAndValidate(%q, %q) = %v, want *ValidationError", tt.id, tt.in, err)
		}
	}
}

// Anyone may hold a property, in either kind of vault: the owner (a business
// or a person) and every person. It is never housing nor a credential.
func TestImmobile_HoldersAndKinds(t *testing.T) {
	persona := tmpVaultKind(t, KindPersona)
	impresa := tmpVaultKind(t, KindImpresa)
	mustSet(t, impresa, "luca:ruolo.tipo", "cliente")
	for _, v := range []*Vault{persona, impresa} {
		for _, id := range []string{"immobile.comune", "luca:immobile.comune", "luca:immobile2.comune"} {
			if err := v.Set(id, "Napoli", ""); err != nil {
				t.Errorf("%s vault: Set(%s) = %v", v.Kind(), id, err)
			}
			if !v.CanHold("immobile.comune", false) || !v.CanHold("immobile.comune", true) {
				t.Errorf("%s vault: CanHold(immobile.comune) is false", v.Kind())
			}
		}
		if err := v.Set("luca:immobile.catasto_categoria", "c8", ""); err == nil {
			t.Errorf("%s vault stored a bad categoria", v.Kind())
		}
	}
	c := GetSchemaCategory("immobile")
	if c == nil || c.Subject != "" || len(c.Kinds) != 0 || c.Scope != ScopePerson || c.OneOnly {
		t.Fatalf("immobile category: %+v", c)
	}
	if IsHousingCategory("immobile") || IsOwnerOnlyCategory("immobile") || isPersonOnlyCategory("immobile") {
		t.Error("immobile is neither housing, nor owner-only, nor person-only")
	}
	// The street and civico are sensitive, like residenza's: a property is often someone's home.
	for _, f := range c.Fields {
		want := "standard"
		if f.ID == "immobile.indirizzo" || f.ID == "immobile.civico" {
			want = "sensitive"
		}
		if f.Sensitivity != want || IsCredentialField(f.ID) || IsCredentialField("luca:"+f.ID) {
			t.Errorf("%s: tier %s, credential %v", f.ID, f.Sensitivity, IsCredentialField(f.ID))
		}
		if got := DefaultSensitivity("luca:immobile2" + f.ID[len("immobile"):]); got != want {
			t.Errorf("DefaultSensitivity of a copy of %s = %s", f.ID, got)
		}
	}
	// The library reads the titles from the schema.
	if f := GetSchemaField("immobile.titolo"); f == nil || !slices.Equal(f.Values, immobileTitoli) {
		t.Errorf("immobile.titolo values: %+v", f)
	}
}

// A client's property in a business vault is located like the business's
// premises, under the scope of its computed fields only.
func TestLocatedImmobile(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	mustSet(t, v, "luca:ruolo.tipo", "cliente")
	address(t, v, "luca:immobile", "Via Alabardieri", "10", "Napoli")
	address(t, v, "luca:immobile2", "Piazza Vanvitelli", "", "Napoli")
	address(t, v, "immobile", "Via Giulio Cesare", "5", "Napoli")
	for id, want := range map[string]string{
		"luca:immobile.municipalita":  "1",
		"luca:immobile.quartiere":     "Chiaia",
		"luca:immobile.distretto_asl": "24/73",
		"luca:immobile2.municipalita": "5",
		"immobile.municipalita":       "10",
		"luca:immobile3.municipalita": "",
		"mario:immobile.municipalita": "",
		"luca:residenza.municipalita": "",
	} {
		f, err := v.Get(id)
		if want == "" {
			if f != nil {
				t.Errorf("%s = %+v", id, f)
			}
			continue
		}
		if err != nil || f == nil || f.Value != want || !f.Derived {
			t.Errorf("%s = %+v, %v; want %q", id, f, err, want)
		}
	}

	only := capped("luca:immobile.municipalita", "sensitive")
	if f, err := v.GetIf("luca:immobile.municipalita", only); err != nil || f == nil || f.Value != "1" {
		t.Errorf("own scope: %+v, %v", f, err)
	}
	if f, _ := v.GetIf("luca:immobile.indirizzo", only); f != nil {
		t.Errorf("street readable: %+v", f)
	}
	loc, err := v.Locate("luca:immobile", only)
	if err != nil || len(loc.Fields) != 1 || loc.Fields["luca:immobile.municipalita"].Value != "1" {
		t.Errorf("Locate: %+v, %v", loc, err)
	}

	// A stored override stands until the address changes.
	past := time.Now().Add(-time.Hour)
	setAt(t, v, "luca:immobile.municipalita", "2", "", past)
	if f, _ := v.Get("luca:immobile.municipalita"); f == nil || f.Value != "1" || !f.Derived {
		t.Errorf("an override older than the address stood: %+v", f)
	}
	mustSet(t, v, "luca:immobile.municipalita", "II")
	if f, _ := v.Get("luca:immobile.municipalita"); f == nil || f.Value != "2" || f.Derived {
		t.Errorf("override: %+v", f)
	}

	// Stored rows only in the listing.
	list, _ := v.List()
	for _, f := range list {
		if IsLocatedField(f.ID) && f.ID != "luca:immobile.municipalita" {
			t.Errorf("List has %s", f.ID)
		}
	}
}
