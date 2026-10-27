package vault

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/crypto"
	"github.com/lovincyrus/personal-vault/internal/store"
)

// capped is the filter of a service token with this scope and tier cap.
func capped(scope, maxTier string) FieldFilter {
	sc := ParseScope(scope)
	return func(id, tier string) bool { return sc.Allows(id) && TierAllows(maxTier, tier) }
}

// setAt stores a field with a chosen update time, to test what is older than what.
func setAt(t *testing.T, v *Vault, id, value, tier string, at time.Time) {
	t.Helper()
	key, err := v.requireUnlocked()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	category, name, _ := strings.Cut(id, ".")
	sub, err := crypto.DeriveSubkey(key, v.salt, category)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(sub)
	enc, err := crypto.EncryptToBase64(sub, []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	if tier == "" {
		tier = DefaultSensitivity(id)
	}
	if err := v.db.SetField(store.Field{ID: id, Category: category, FieldName: name, Value: enc, Sensitivity: tier, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
}

func address(t *testing.T, v *Vault, prefix, street, civico, comune string) {
	t.Helper()
	mustSet(t, v, prefix+".indirizzo", street)
	if civico != "" {
		mustSet(t, v, prefix+".civico", civico)
	}
	mustSet(t, v, prefix+".comune", comune)
}

func TestLocatedComputedFromTheAddress(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Alabardieri", "10", "Napoli")
	for id, want := range map[string]string{
		"residenza.quartiere":     "Chiaia",
		"residenza.municipalita":  "1",
		"residenza.distretto_asl": "24/73",
	} {
		f, err := v.Get(id)
		if err != nil || f == nil {
			t.Fatalf("%s: %v, %v", id, f, err)
		}
		if f.Value != want || !f.Derived || f.Sensitivity != "standard" || f.FieldName != strings.TrimPrefix(id, "residenza.") {
			t.Errorf("%s = %+v, want %q derived", id, f, want)
		}
	}
	// Nothing was stored.
	list, _ := v.List()
	for _, f := range list {
		if IsLocatedField(f.ID) {
			t.Errorf("computed field %s was stored", f.ID)
		}
	}
}

func TestLocatedStreetAcrossBoundaries(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Toledo", "", "Napoli")

	_, err := v.Get("residenza.municipalita")
	var nr *NotResolvedError
	if !errors.As(err, &nr) {
		t.Fatalf("want NotResolvedError, got %v", err)
	}
	if want := []string{"San Ferdinando", "Montecalvario", "San Giuseppe"}; !slices.Equal(nr.Choices, want) {
		t.Errorf("choices %v, want %v", nr.Choices, want)
	}
	if strings.Contains(strings.ToLower(nr.Error()), "toledo") {
		t.Errorf("the street must not be echoed: %v", nr)
	}

	// A civico in a known range settles it.
	mustSet(t, v, "residenza.civico", "324")
	if f, err := v.Get("residenza.municipalita"); err != nil || f.Value != "2" {
		t.Errorf("civico 324: %+v, %v", f, err)
	}
	if f, _ := v.Get("residenza.quartiere"); f == nil || f.Value != "Montecalvario" {
		t.Errorf("quartiere %+v", f)
	}
	// A civico in no range settles nothing; the stored quartiere does.
	mustSet(t, v, "residenza.civico", "999")
	if _, err := v.Get("residenza.municipalita"); !errors.As(err, &nr) {
		t.Fatalf("civico 999: %v", err)
	}
	mustSet(t, v, "residenza.quartiere", "S. Ferdinando")
	f, err := v.Get("residenza.municipalita")
	if err != nil || f.Value != "1" || !f.Derived {
		t.Fatalf("with quartiere: %+v, %v", f, err)
	}
	if d, _ := v.Get("residenza.distretto_asl"); d == nil || d.Value != "24/73" {
		t.Errorf("distretto %+v", d)
	}
	// The stored quartiere is read as stored.
	if q, _ := v.Get("residenza.quartiere"); q == nil || q.Derived || q.Value != "San Ferdinando" {
		t.Errorf("quartiere %+v", q)
	}
}

func TestLocatedNotInNapoli(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Alabardieri", "", "Pozzuoli")
	_, err := v.Get("residenza.municipalita")
	var nr *NotResolvedError
	if !errors.As(err, &nr) || !strings.Contains(nr.Reason, "not in Napoli") {
		t.Fatalf("%v", err)
	}
	mustSet(t, v, "residenza.indirizzo", "Via Inesistente Qwerty")
	mustSet(t, v, "residenza.comune", "Napoli")
	if _, err := v.Get("residenza.municipalita"); !errors.As(err, &nr) || !strings.Contains(nr.Reason, "not in the stradario") {
		t.Fatalf("unknown street: %v", err)
	}
	// No address at all.
	v2, _ := tmpVault(t)
	if _, err := v2.Get("residenza.municipalita"); !errors.As(err, &nr) {
		t.Errorf("no address: %v", err)
	}
}

func TestLocatedOverride(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Alabardieri", "", "Napoli")
	mustSet(t, v, "residenza.municipalita", "Municipalità II")
	f, err := v.Get("residenza.municipalita")
	if err != nil || f.Value != "2" || f.Derived {
		t.Fatalf("%+v, %v", f, err)
	}
	// The distretto follows the stored municipalità.
	if d, _ := v.Get("residenza.distretto_asl"); d == nil || d.Value != "31" || !d.Derived {
		t.Errorf("distretto %+v", d)
	}
	loc, err := v.Locate("residenza", nil)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Fields["residenza.municipalita"].Source != "stored" || loc.Fields["residenza.distretto_asl"].Source != "computed" {
		t.Errorf("%+v", loc.Fields)
	}
	if len(loc.Notes) != 1 || !strings.Contains(loc.Notes[0], "differs") {
		t.Errorf("notes %v", loc.Notes)
	}
	// A stored distretto overrides too.
	mustSet(t, v, "residenza.distretto_asl", "DS 26")
	if d, _ := v.Get("residenza.distretto_asl"); d == nil || d.Value != "26" || d.Derived {
		t.Errorf("distretto %+v", d)
	}
}

func TestLocatedStaleOverrideIsIgnored(t *testing.T) {
	v, _ := tmpVault(t)
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)
	setAt(t, v, "residenza.municipalita", "7", "", t0)
	setAt(t, v, "residenza.distretto_asl", "30", "", t0)
	setAt(t, v, "residenza.quartiere", "Scampia", "", t0)

	// No address yet: nothing to be older than, the override stands.
	if f, _ := v.Get("residenza.municipalita"); f == nil || f.Value != "7" || f.Derived {
		t.Fatalf("%+v", f)
	}
	// The address is written at the same second: not older.
	setAt(t, v, "residenza.indirizzo", "Via Alabardieri", "", t0)
	setAt(t, v, "residenza.comune", "Napoli", "", t0)
	if f, _ := v.Get("residenza.municipalita"); f == nil || f.Value != "7" {
		t.Fatalf("same second: %+v", f)
	}
	// The address changes: the stored values are older than it.
	setAt(t, v, "residenza.indirizzo", "Piazza Vanvitelli", "", t0.Add(time.Minute))
	for id, want := range map[string]string{
		"residenza.municipalita":  "5",
		"residenza.quartiere":     "Vomero",
		"residenza.distretto_asl": "27",
	} {
		if f, err := v.Get(id); err != nil || f.Value != want || !f.Derived {
			t.Errorf("%s: %+v, %v; want computed %q", id, f, err, want)
		}
	}
	loc, _ := v.Locate("residenza", nil)
	if len(loc.Notes) != 3 {
		t.Errorf("notes %v", loc.Notes)
	}
	// The category read shows the computed values, never the stale rows.
	fields, err := v.GetByCategory("residenza", nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]FieldInfo{}
	for _, f := range fields {
		if IsLocatedField(f.ID) {
			if _, dup := seen[f.ID]; dup {
				t.Errorf("%s listed twice", f.ID)
			}
			seen[f.ID] = f
		}
	}
	if len(seen) != 3 || seen["residenza.municipalita"].Value != "5" || !seen["residenza.municipalita"].Derived {
		t.Errorf("category read: %+v", seen)
	}
	// Storing it again, now, makes it stand.
	mustSet(t, v, "residenza.municipalita", "6")
	if f, _ := v.Get("residenza.municipalita"); f == nil || f.Value != "6" || f.Derived {
		t.Errorf("%+v", f)
	}
}

func TestLocatedDistrettoOlderThanItsMunicipalita(t *testing.T) {
	v, _ := tmpVault(t)
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)
	setAt(t, v, "residenza.distretto_asl", "30", "", t0)
	setAt(t, v, "residenza.municipalita", "2", "", t0.Add(time.Minute))
	if f, _ := v.Get("residenza.distretto_asl"); f == nil || f.Value != "31" || !f.Derived {
		t.Errorf("%+v", f)
	}
}

func TestLocatedStoredValuesAreValidated(t *testing.T) {
	v, _ := tmpVault(t)
	for id, tests := range map[string]map[string]string{
		"residenza.municipalita": {"3": "3", "10": "10", "VII": "7", " II ": "2", "2ª": "2", "municipalità 4": "4", "Mun. 5": "5", "02": "2", "z": "!", "0": "!", "11": "!", "XI": "!"},
		"residenza.distretto_asl": {"24/73": "24/73", "24": "24/73", "73": "24/73", "DS 31": "31", "Distretto sanitario n. 33": "33",
			"n° 25": "25", "34": "!", "23": "!", "": "!", "abc": "!"},
		"residenza.quartiere": {"Vomero": "Vomero", "s. carlo all'arena": "San Carlo all'Arena", "S.Giov.a Ted": "San Giovanni a Teduccio",
			"Marianella": "Piscinola", "Colli Aminei": "!", "Centro": "!"},
	} {
		for in, want := range tests {
			got, err := NormalizeAndValidate(id, in)
			switch {
			case want == "!" && err == nil:
				t.Errorf("%s %q accepted as %q", id, in, got)
			case want != "!" && (err != nil || got != want):
				t.Errorf("%s %q = %q, %v; want %q", id, in, got, err, want)
			}
		}
	}
	if err := v.Set("residenza.municipalita", "99", ""); err == nil {
		t.Error("Set accepted municipalità 99")
	}
}

func TestLocatedReadUnderItsOwnScope(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Alabardieri", "10", "Napoli")

	only := capped("residenza.municipalita", "sensitive")
	f, err := v.GetIf("residenza.municipalita", only)
	if err != nil || f == nil || f.Value != "1" {
		t.Fatalf("%+v, %v", f, err)
	}
	// The street, and the other computed fields, stay out of reach.
	if g, _ := v.GetIf("residenza.indirizzo", only); g != nil {
		t.Errorf("street readable: %+v", g)
	}
	if g, _ := v.GetIf("residenza.distretto_asl", only); g != nil {
		t.Errorf("out of scope: %+v", g)
	}
	fields, err := v.GetByCategory("residenza", only)
	if err != nil || len(fields) != 1 || fields[0].ID != "residenza.municipalita" || fields[0].Value != "1" {
		t.Errorf("category read %+v, %v", fields, err)
	}
	ctx, err := v.GetContext(only)
	if err != nil {
		t.Fatal(err)
	}
	if got := ctx.Categories["residenza"]; len(got) != 1 || got[0].ID != "residenza.municipalita" {
		t.Errorf("context %+v", ctx.Categories)
	}
	// A token capped below standard sees nothing computed.
	if g, _ := v.GetIf("residenza.municipalita", capped("residenza.*", "public")); g != nil {
		t.Errorf("public cap: %+v", g)
	}
	// The audit log names the computed field, never the street.
	log, _ := v.AuditLog(50)
	for _, e := range log {
		if strings.Contains(e.Scope, "indirizzo") && e.Action == "read" {
			t.Errorf("street read in the audit log: %+v", e)
		}
	}
}

func TestLocatedPersonalAddressStaysHidden(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Alabardieri", "", "Napoli")
	if err := v.SetSensitivity("residenza.indirizzo", TierPersonal); err != nil {
		t.Fatal(err)
	}
	token := capped("residenza.*", "sensitive")
	_, err := v.GetIf("residenza.municipalita", token)
	var nr *NotResolvedError
	if !errors.As(err, &nr) || strings.Contains(nr.Reason, "Napoli") && !strings.Contains(nr.Reason, "no street") {
		t.Errorf("a personal street must not feed a token's lookup: %v", err)
	}
	// Whoever may see personal items gets it.
	if f, err := v.GetIf("residenza.municipalita", capped("residenza.*", TierPersonal)); err != nil || f == nil || f.Value != "1" {
		t.Errorf("%+v, %v", f, err)
	}
}

func TestLocatedHiddenStoredRowIsNotReplacedByTheComputedOne(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Alabardieri", "", "Napoli")
	mustSet(t, v, "residenza.municipalita", "2")
	if err := v.SetSensitivity("residenza.municipalita", TierPersonal); err != nil {
		t.Fatal(err)
	}
	token := capped("residenza.*", "sensitive")
	if f, err := v.GetIf("residenza.municipalita", token); err != nil || f != nil {
		t.Errorf("a hidden override read as %+v, %v", f, err)
	}
	// Nor is the distretto computed from a municipalità the caller can't see.
	if f, err := v.GetIf("residenza.distretto_asl", token); err != nil || f != nil {
		t.Errorf("distretto %+v, %v", f, err)
	}
	fields, _ := v.GetByCategory("residenza", token)
	for _, f := range fields {
		if f.ID == "residenza.municipalita" || f.ID == "residenza.distretto_asl" {
			t.Errorf("%s shown", f.ID)
		}
	}
}

func TestLocatedKindsAndPeople(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "domicilio", "Piazza Vanvitelli", "", "Napoli")
	address(t, v, "leo:residenza", "Via Giulio Cesare", "10", "Napoli")
	for id, want := range map[string]string{
		"domicilio.municipalita":        "5",
		"leo:residenza.municipalita":    "10",
		"leo:residenza.distretto_asl":   "25",
		"leo:residenza.quartiere":       "Fuorigrotta",
		"leo:domicilio.municipalita":    "",
		"residenza.municipalita":        "",
		"leo:residenza2.municipalita":   "",
		"leo:residenza.municipalita.xx": "",
	} {
		f, err := v.Get(id)
		if want == "" {
			if f != nil {
				t.Errorf("%s = %+v", id, f)
			}
			continue
		}
		if err != nil || f == nil || f.Value != want {
			t.Errorf("%s = %+v, %v; want %q", id, f, err, want)
		}
	}
	// A person's own rows read only through their prefix.
	if f, _ := v.Get("leo:residenza.municipalita"); f == nil || f.Category != "leo:residenza" {
		t.Errorf("%+v", f)
	}
}

func TestLocatedBusinessVault(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	address(t, v, "locale", "Via Alabardieri", "", "Napoli")
	address(t, v, "locale2", "Piazza Vanvitelli", "", "Napoli")
	mustSet(t, v, "business.sede_legale_indirizzo", "Via Giulio Cesare")
	mustSet(t, v, "business.sede_legale_civico", "5")
	mustSet(t, v, "business.sede_legale_comune", "Napoli")
	for id, want := range map[string]string{
		"locale.municipalita":                "1",
		"locale.distretto_asl":               "24/73",
		"locale2.municipalita":               "5",
		"locale2.quartiere":                  "Vomero",
		"business.sede_legale_municipalita":  "10",
		"business.sede_legale_distretto_asl": "25",
		"business.sede_legale_quartiere":     "Fuorigrotta",
		"business.municipalita":              "",
		"residenza.municipalita":             "",
	} {
		f, err := v.Get(id)
		if want == "" {
			if f != nil {
				t.Errorf("%s = %+v", id, f)
			}
			continue
		}
		if err != nil || f == nil || f.Value != want {
			t.Errorf("%s = %+v, %v; want %q", id, f, err, want)
		}
	}
	// The business category lists the sede legale's computed fields among its own.
	fields, _ := v.GetByCategory("business", nil)
	n := 0
	for _, f := range fields {
		if f.Derived {
			n++
		}
	}
	if n != 3 {
		t.Errorf("%d computed fields in business, want 3", n)
	}
}

func TestLocatedInContextAndNotInList(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Alabardieri", "", "Napoli")
	ctx, err := v.GetContext(nil)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, f := range ctx.Categories["residenza"] {
		if f.Derived {
			found++
		}
	}
	if found != 3 {
		t.Errorf("%d computed fields in the context, want 3", found)
	}
	// Stored data only: the listing, the export of a person and the status.
	list, _ := v.List()
	for _, f := range list {
		if IsLocatedField(f.ID) {
			t.Errorf("List has %s", f.ID)
		}
	}
	st, _ := v.Status()
	if st.Categories["residenza"] != 2 {
		t.Errorf("status counts %d residenza fields, want the 2 stored", st.Categories["residenza"])
	}
}

func TestLocateSaysWhy(t *testing.T) {
	v, _ := tmpVault(t)
	if _, err := v.Locate("identity", nil); !errors.Is(err, ErrNotLocatable) {
		t.Errorf("identity: %v", err)
	}
	address(t, v, "residenza", "Via Toledo", "", "Napoli")
	loc, err := v.Locate("residenza", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(loc.Fields) != 0 || loc.Reason == "" || len(loc.Choices) != 3 {
		t.Errorf("%+v", loc)
	}
	mustSet(t, v, "residenza.civico", "258")
	loc, _ = v.Locate("residenza", nil)
	if loc.Basis != "civico" || loc.Fields["residenza.municipalita"].Value != "1" || loc.Reason != "" {
		t.Errorf("%+v", loc)
	}
	loc, _ = v.Locate("residenza", capped("residenza.*", "sensitive"))
	if len(loc.Fields) != 3 {
		t.Errorf("%+v", loc)
	}
}

func TestLocatedTarget(t *testing.T) {
	for in, want := range map[string]struct {
		category, municipalita string
	}{
		"residenza":         {"residenza", "residenza.municipalita"},
		"leo:residenza":     {"leo:residenza", "leo:residenza.municipalita"},
		"locale2":           {"locale2", "locale2.municipalita"},
		"sede_legale":       {"business", "business.sede_legale_municipalita"},
		"rossi:sede_legale": {"rossi:business", "rossi:business.sede_legale_municipalita"},
	} {
		category, ids, ok := LocatedTarget(in)
		if !ok || category != want.category || ids[locMunicipalita] != want.municipalita {
			t.Errorf("LocatedTarget(%q) = %q, %v, %v", in, category, ids, ok)
		}
	}
	for _, in := range []string{"identity", "Residenza", "residenza.indirizzo", "", "leo:", "tari"} {
		if _, _, ok := LocatedTarget(in); ok {
			t.Errorf("LocatedTarget(%q) accepted", in)
		}
	}
}

func TestLocatedSchemaFields(t *testing.T) {
	for _, k := range locatedKinds {
		for _, name := range k.outputs() {
			f := GetSchemaField(k.category + "." + name)
			if f == nil || !f.Derived || f.Sensitivity != "standard" {
				t.Errorf("%s.%s: %+v", k.category, name, f)
			}
		}
		for _, name := range []string{k.street, k.civico, k.comune} {
			if f := GetSchemaField(k.category + "." + name); f == nil || f.Derived {
				t.Errorf("%s.%s is an input: %+v", k.category, name, f)
			}
		}
	}
	derived := 0
	for _, c := range RecommendedSchema.Categories {
		for _, f := range c.Fields {
			if f.Derived {
				derived++
				if !IsLocatedField(f.ID) {
					t.Errorf("%s is derived in the schema but not located", f.ID)
				}
			}
		}
	}
	if derived != 12 {
		t.Errorf("%d derived fields, want 12", derived)
	}
}

func TestLocatedNotesSayOnlyWhatTheCallerMaySee(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Alabardieri", "", "Napoli")
	mustSet(t, v, "residenza.municipalita", "2")

	// The owner is told the override disagrees with the stradario.
	if loc, _ := v.Locate("residenza", nil); len(loc.Notes) != 1 {
		t.Fatalf("notes for the owner: %v", loc.Notes)
	}
	// A caller that can't see the stored value is told nothing about it, whether it
	// is hidden by the personal tier or by the token's cap.
	if err := v.SetSensitivity("residenza.municipalita", TierPersonal); err != nil {
		t.Fatal(err)
	}
	loc, err := v.Locate("residenza", capped("residenza.*", "sensitive"))
	if err != nil || len(loc.Notes) != 0 {
		t.Errorf("personal override: %v, %v", loc, err)
	}
	if err := v.SetSensitivity("residenza.municipalita", "standard"); err != nil {
		t.Fatal(err)
	}
	if loc, _ := v.Locate("residenza", capped("residenza.*", "public")); len(loc.Notes) != 0 || len(loc.Fields) != 0 {
		t.Errorf("public cap: %+v", loc)
	}
	// Nor is a hidden stale row said to exist.
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)
	setAt(t, v, "residenza.municipalita", "7", TierPersonal, t0)
	if loc, _ := v.Locate("residenza", capped("residenza.*", "sensitive")); len(loc.Notes) != 0 {
		t.Errorf("hidden stale row: %v", loc.Notes)
	}
	loc, _ = v.Locate("residenza", nil)
	if len(loc.Notes) != 1 || !strings.Contains(loc.Notes[0], "older than the address") {
		t.Errorf("owner: %v", loc.Notes)
	}
}

func TestLocatedHiddenStoredQuartiereDoesNotFeedTheLookup(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Toledo", "", "Napoli")
	mustSet(t, v, "residenza.quartiere", "San Ferdinando")
	if err := v.SetSensitivity("residenza.quartiere", TierPersonal); err != nil {
		t.Fatal(err)
	}
	token := capped("residenza.*", "sensitive")
	var nr *NotResolvedError
	if _, err := v.GetIf("residenza.municipalita", token); !errors.As(err, &nr) {
		t.Errorf("a hidden quartiere settled the municipalità: %v", err)
	}
	if f, _ := v.GetIf("residenza.quartiere", token); f != nil {
		t.Errorf("hidden quartiere read: %+v", f)
	}
	// Whoever may see it is answered with it.
	if f, err := v.GetIf("residenza.municipalita", capped("residenza.*", TierPersonal)); err != nil || f == nil || f.Value != "1" {
		t.Errorf("%+v, %v", f, err)
	}
	// An ordinary stored quartiere out of the token's scope still settles it
	// for a token scoped to the municipalità alone.
	if err := v.SetSensitivity("residenza.quartiere", "standard"); err != nil {
		t.Fatal(err)
	}
	if f, err := v.GetIf("residenza.municipalita", capped("residenza.municipalita", "standard")); err != nil || f == nil || f.Value != "1" {
		t.Errorf("%+v, %v", f, err)
	}
}

// A grant the user approved in person for the computed fields reaches personal
// items, so a personal street feeds it, although the street is not in its scope.
func TestLocatedPersonalStreetFeedsAGrantThatReachesPersonal(t *testing.T) {
	v, _ := tmpVault(t)
	address(t, v, "residenza", "Via Alabardieri", "", "Napoli")
	if err := v.SetSensitivity("residenza.indirizzo", TierPersonal); err != nil {
		t.Fatal(err)
	}
	grant := capped("residenza.quartiere,residenza.municipalita,residenza.distretto_asl", TierPersonal)
	if f, err := v.GetIf("residenza.municipalita", grant); err != nil || f == nil || f.Value != "1" {
		t.Errorf("%+v, %v", f, err)
	}
	token := capped("residenza.quartiere,residenza.municipalita,residenza.distretto_asl", "sensitive")
	if f, _ := v.GetIf("residenza.municipalita", token); f != nil {
		t.Errorf("a token read a municipalità from a personal street: %+v", f)
	}
}

func TestStoredContextKeepsAStaleStoredRow(t *testing.T) {
	v, _ := tmpVault(t)
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)
	setAt(t, v, "residenza.municipalita", "7", "", t0)
	address(t, v, "residenza", "Via Alabardieri", "", "Napoli")

	stored, err := v.GetStoredContext(nil)
	if err != nil {
		t.Fatal(err)
	}
	var got *FieldInfo
	for i, f := range stored.Categories["residenza"] {
		if f.Derived {
			t.Errorf("%s is computed", f.ID)
		}
		if f.ID == "residenza.municipalita" {
			got = &stored.Categories["residenza"][i]
		}
	}
	if got == nil || got.Value != "7" {
		t.Errorf("the stale stored row is missing from the stored context: %+v", got)
	}
	// The ordinary context shows the value in force instead.
	ctx, _ := v.GetContext(nil)
	for _, f := range ctx.Categories["residenza"] {
		if f.ID == "residenza.municipalita" && (f.Value != "1" || !f.Derived) {
			t.Errorf("context: %+v", f)
		}
	}
}

func TestLocatedNaturalSpellings(t *testing.T) {
	for id, tests := range map[string]map[string]string{
		"residenza.municipalita": {
			"Municipalità n. 3": "3", "Municipalita' 3": "3", "III Municipalità": "3", "3ª municipalità": "3", "Mun 5": "5",
			"municipalità X": "10", "N. 4": "4", "IV": "4", "V": "5", "4.": "4",
			"n. 11": "!", "municipalità": "!", "": "!", "3 4": "!", "XI": "!", "IIII": "!",
		},
		"residenza.distretto_asl": {
			"ds31": "31", "D.S. 25": "25", "DS-27": "27", "ds 24/73": "24/73", "Distretto sanitario n. 33": "33",
			"distretto 32": "32", "n.29": "29", "24 / 73": "24/73", "DS 3": "!", "ds34": "!", "24/74": "!", "distretto": "!",
		},
	} {
		for in, want := range tests {
			got, err := NormalizeAndValidate(id, in)
			switch {
			case want == "!" && err == nil:
				t.Errorf("%s %q accepted as %q", id, in, got)
			case want != "!" && (err != nil || got != want):
				t.Errorf("%s %q = %q, %v; want %q", id, in, got, err, want)
			}
		}
	}
	_, err := NormalizeAndValidate("residenza.distretto_asl", "99")
	if err == nil || strings.Contains(err.Error(), "ASL ASL") {
		t.Errorf("message: %v", err)
	}
}
