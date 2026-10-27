package main

import (
	"os"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/napoli"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

var residenzaIDs = [3]string{"residenza.quartiere", "residenza.municipalita", "residenza.distretto_asl"}

func TestFormatLocated(t *testing.T) {
	for _, c := range []struct {
		name     string
		loc      vault.Located
		resolved bool
		want     []string
		not      []string
	}{
		{
			"computed from the street",
			vault.Located{Basis: "street", Fields: map[string]vault.LocatedField{
				"residenza.quartiere":     {Value: "Chiaia", Source: "computed"},
				"residenza.municipalita":  {Value: "1", Source: "computed"},
				"residenza.distretto_asl": {Value: "24/73", Source: "computed"},
			}},
			true,
			[]string{"Municipalità 1 · Chiaia · ASL Napoli 1 Centro, distretto 24/73\n", "from the street"},
			[]string{"override", "check"},
		},
		{
			"computed from the civico",
			vault.Located{Basis: "civico", Fields: map[string]vault.LocatedField{
				"residenza.municipalita": {Value: "2", Source: "computed"},
			}},
			true,
			[]string{"Municipalità 2\n", "2003 stradario", "renumbered"},
			[]string{"·"},
		},
		{
			"an override, with a note",
			vault.Located{Notes: []string{"the stored municipalità (2) differs from the stradario's (1)"}, Fields: map[string]vault.LocatedField{
				"residenza.municipalita":  {Value: "2", Source: "stored"},
				"residenza.distretto_asl": {Value: "31", Source: "stored"},
			}},
			true,
			[]string{"municipalità: stored by you", "distretto: stored by you", "note: the stored municipalità (2) differs"},
			nil,
		},
		{
			"a partial street",
			vault.Located{Basis: "street", Partial: true, Fields: map[string]vault.LocatedField{
				"residenza.municipalita": {Value: "5", Source: "computed"},
			}},
			true,
			[]string{"matched on part of its name"},
			nil,
		},
		{
			"a street across a boundary",
			vault.Located{
				Reason:  "the street crosses municipalità boundaries; the quartiere decides",
				Choices: []string{"San Ferdinando", "Montecalvario"},
				Fields:  map[string]vault.LocatedField{},
			},
			false,
			[]string{
				"The street crosses municipalità boundaries; the quartiere decides.\n",
				"Which quartiere is the address in?",
				`pvault set residenza.quartiere "San Ferdinando"   → municipalità 1, distretto 24/73`,
				`pvault set residenza.quartiere "Montecalvario"   → municipalità 2, distretto 31`,
			},
			nil,
		},
		{
			"not in Napoli",
			vault.Located{Reason: "the address is not in Napoli", Fields: map[string]vault.LocatedField{}},
			false,
			[]string{"The address is not in Napoli.\n", "pvault set residenza.municipalita <1-10>"},
			[]string{"Which quartiere"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, resolved := formatLocated(c.loc, residenzaIDs)
			if resolved != c.resolved {
				t.Errorf("resolved %v:\n%s", resolved, out)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, n := range c.not {
				if strings.Contains(out, n) {
					t.Errorf("unexpected %q in:\n%s", n, out)
				}
			}
		})
	}
}

func TestFormatLookup(t *testing.T) {
	q := napoli.Query{Street: "Via Toledo", Civico: "999"}
	out, resolved := formatLookup(napoli.Lookup(q), q)
	if resolved || !strings.Contains(out, `pvault locate --street "Via Toledo" --civico "999" --quartiere "Montecalvario"   → municipalità 2, distretto 31`) {
		t.Errorf("%v\n%s", resolved, out)
	}
	q = napoli.Query{Street: "Via Tasso"}
	out, resolved = formatLookup(napoli.Lookup(q), q)
	if resolved || !strings.Contains(out, "Which quartiere") {
		t.Errorf("%v\n%s", resolved, out)
	}
	q = napoli.Query{Street: "Via Cilea"}
	out, resolved = formatLookup(napoli.Lookup(q), q)
	if !resolved || !strings.Contains(out, "matched: VIA CILEA FRANCESCO") || !strings.Contains(out, "Municipalità 5 · Vomero") {
		t.Errorf("%v\n%s", resolved, out)
	}
	q = napoli.Query{Street: "Via Qwerty Inesistente"}
	if out, resolved = formatLookup(napoli.Lookup(q), q); resolved || !strings.Contains(out, "stradario fits") {
		t.Errorf("%v\n%s", resolved, out)
	}
}

func locate(t *testing.T, args ...string) (string, bool, error) {
	t.Helper()
	cliToken = "" // each command asks for its own grant, as a new process would
	t.Cleanup(func() { cliToken = "" })
	return runLocate(args)
}

func TestLocate_EndToEnd(t *testing.T) {
	v, argsFile := touchIDVault(t, map[string]string{
		"residenza.indirizzo":     "Via Alabardieri",
		"residenza.civico":        "10",
		"residenza.comune":        "Napoli",
		"leo:residenza.indirizzo": "Via Toledo",
		"leo:residenza.comune":    "Napoli",
		"domicilio.indirizzo":     "Via Alabardieri",
		"domicilio.comune":        "Pozzuoli",
		"identity.full_name":      "Mario Rossi",
	})

	out, resolved, err := locate(t, "residenza")
	if err != nil || !resolved || !strings.HasPrefix(out, "Municipalità 1 · Chiaia · ASL Napoli 1 Centro, distretto 24/73\n") {
		t.Fatalf("%v %v\n%s", err, resolved, out)
	}
	// One grant, for the three computed fields: the street is never asked for.
	args, _ := os.ReadFile(argsFile)
	if got := string(args); got != "grant --scope residenza.quartiere,residenza.municipalita,residenza.distretto_asl --access read\n" {
		t.Errorf("grant:\n%s", got)
	}

	// A person's street across a boundary: the quartiere to store.
	out, resolved, err = locate(t, "leo:residenza")
	if err != nil || resolved || !strings.Contains(out, `pvault set leo:residenza.quartiere "Montecalvario"   → municipalità 2, distretto 31`) {
		t.Fatalf("%v %v\n%s", err, resolved, out)
	}
	if strings.Contains(strings.ToLower(out), "toledo") {
		t.Errorf("the street is printed:\n%s", out)
	}
	if err := v.Set("leo:residenza.quartiere", "Montecalvario", ""); err != nil {
		t.Fatal(err)
	}
	out, resolved, err = locate(t, "leo:residenza")
	if err != nil || !resolved || !strings.Contains(out, "Municipalità 2 · Montecalvario") || !strings.Contains(out, "from the quartiere") {
		t.Fatalf("%v %v\n%s", err, resolved, out)
	}

	// Not in Napoli.
	out, resolved, err = locate(t, "domicilio")
	if err != nil || resolved || !strings.Contains(out, "not in Napoli") {
		t.Fatalf("%v %v\n%s", err, resolved, out)
	}

	// An override, read back as stored.
	if err := v.Set("residenza.municipalita", "VII", ""); err != nil {
		t.Fatal(err)
	}
	out, _, err = locate(t, "residenza")
	if err != nil || !strings.Contains(out, "Municipalità 7") || !strings.Contains(out, "stored by you") || !strings.Contains(out, "note: the stored municipalità (7) differs from the stradario's (1)") {
		t.Fatalf("%v\n%s", err, out)
	}

	// What isn't an address.
	for _, bad := range []string{"identity", "residenza.indirizzo", "Residenza"} {
		if _, _, err := locate(t, bad); err == nil || !strings.Contains(err.Error(), "usage: pvault locate") {
			t.Errorf("locate %s: %v", bad, err)
		}
	}
	if _, _, err := locate(t, "residenza", "domicilio"); err == nil {
		t.Error("two targets accepted")
	}
}

func TestLocate_AddressTypedHereNeedsNoVault(t *testing.T) {
	// No server, no grant: the command must not touch either.
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:1")
	cliToken = ""
	out, resolved, err := runLocate([]string{"--street", "Via Toledo", "--civico", "258"})
	if err != nil || !resolved || !strings.Contains(out, "Municipalità 1 · San Ferdinando") {
		t.Fatalf("%v %v\n%s", err, resolved, out)
	}
	out, resolved, err = runLocate([]string{"--street", "Via Toledo", "--quartiere", "Montecalvario"})
	if err != nil || !resolved || !strings.Contains(out, "Municipalità 2 · Montecalvario") {
		t.Fatalf("%v %v\n%s", err, resolved, out)
	}
	for _, bad := range [][]string{
		{"--civico", "5"},
		{"--street"},
		{"--street", "Via Toledo", "--street", "Via Foria"},
		{"--street", "Via Toledo", "--nope", "x"},
		{"--street", "Via Toledo", "residenza"},
		{"--street", "  "},
	} {
		if _, _, err := runLocate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestLocate_ListedAsComputedAndNotExported(t *testing.T) {
	touchIDVault(t, map[string]string{
		"residenza.indirizzo": "Via Alabardieri",
		"residenza.comune":    "Napoli",
	})
	// Asked for by name it is read, like any field.
	cliToken = ""
	ids, values, err := getFields([]string{"residenza.municipalita", "residenza.indirizzo"}, true)
	if err != nil || *values[ids[0]] != "1" || *values[ids[1]] != "Via Alabardieri" {
		t.Fatalf("%v %v %v", ids, values, err)
	}
	// A whole category read includes what the vault computes in it, and leaves it
	// out when it can't (an address outside Napoli, a street across a boundary).
	cliToken = ""
	ids, values, err = getFields([]string{"residenza.*"}, true)
	if err != nil || strings.Join(ids, ",") != "residenza.comune,residenza.distretto_asl,residenza.indirizzo,residenza.municipalita,residenza.quartiere" ||
		*values["residenza.municipalita"] != "1" || *values["residenza.distretto_asl"] != "24/73" {
		t.Fatalf("category read: %v %v %v", ids, values, err)
	}
	// A category listing shows them as computed; the export holds stored data only.
	cliToken = ""
	if out := runCmd(t, cmdExport, "export"); strings.Contains(out, "municipalita") || strings.Contains(out, "derived") || !strings.Contains(out, "Via Alabardieri") {
		t.Errorf("export:\n%s", out)
	}
	cliToken = ""
	out := runCmd(t, cmdList, "list", "residenza")
	if !strings.Contains(out, "residenza.municipalita   1 [computed]") || strings.Contains(out, "residenza.comune         Napoli [computed]") || strings.Count(out, "[computed]") != 3 {
		t.Errorf("list:\n%s", out)
	}
}
