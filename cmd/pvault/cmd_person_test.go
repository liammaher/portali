package main

import (
	"os"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func TestPersonAddFields(t *testing.T) {
	fields, err := personAddFields("giulia", vault.KindPersona, []string{"--nome", "Giulia", "--relazione", "moglie", "--sesso", "F"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range fields {
		got = append(got, f[0]+"="+f[1])
	}
	// relazione first, so the member exists before the rest is written.
	if want := "giulia:famiglia.relazione=moglie,giulia:identity.first_name=Giulia,giulia:identity.sex=F"; strings.Join(got, ",") != want {
		t.Fatalf("fields = %v, want %s", got, want)
	}
	for _, bad := range [][]string{{"--nome", "Giulia"}, {"--relazione"}, {"--relazione", "x", "--relazione", "y"}, {"--eta", "3"},
		{"--relazione", "amico"}, {"--relazione", "moglie", "--codice-fiscale", "XYZ"}, {"--relazione", "figlio", "--data-nascita", "31/02/2020"}} {
		if _, err := personAddFields("giulia", vault.KindPersona, bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	// A business vault: ruolo instead of relazione, written first.
	fields, err = personAddFields("rossi", vault.KindImpresa, []string{"--nome", "Mario", "--ruolo", "cliente"})
	if err != nil {
		t.Fatal(err)
	}
	if fields[0] != [2]string{"rossi:ruolo.tipo", "cliente"} || len(fields) != 2 {
		t.Fatalf("business fields = %v", fields)
	}
	for _, bad := range [][]string{{"--nome", "Mario"}, {"--relazione", "figlio"}, {"--ruolo", "cliente", "--relazione", "figlio"}, {"--ruolo", "amico"}} {
		if _, err := personAddFields("rossi", vault.KindImpresa, bad); err == nil {
			t.Errorf("business vault: %v accepted", bad)
		}
	}
	if _, err := personAddFields("giulia", vault.KindPersona, []string{"--relazione", "moglie", "--ruolo", "socio"}); err == nil {
		t.Error("personal vault accepted --ruolo")
	}
}

// member add goes through one Touch ID approval scoped to the new member,
// and never overwrites someone already there.
func TestPersonAdd_EndToEnd(t *testing.T) {
	v, argsFile := touchIDVault(t, map[string]string{"leo:famiglia.relazione": "figlio"})

	needGrant("giulia:famiglia.*,giulia:identity.*", "append")
	fields, _ := personAddFields("giulia", vault.KindPersona, []string{"--relazione", "moglie", "--nome", "Giulia"})
	if err := addPerson("giulia", fields); err != nil {
		t.Fatal(err)
	}
	if f, err := v.Get("giulia:famiglia.relazione"); err != nil || f.Value != "coniuge" {
		t.Fatalf("giulia:famiglia.relazione = %+v, %v", f, err)
	}
	data, _ := os.ReadFile(argsFile)
	if want := "grant --scope giulia:famiglia.*,giulia:identity.* --access append\n"; string(data) != want {
		t.Fatalf("helper calls:\n%s\nwant:\n%s", data, want)
	}

	cliToken = ""
	needGrant("leo:famiglia.*,leo:identity.*", "append")
	fields, _ = personAddFields("leo", vault.KindPersona, []string{"--relazione", "nipote"})
	if err := addPerson("leo", fields); err == nil || !strings.Contains(err.Error(), "already has leo:famiglia.relazione") {
		t.Fatalf("adding an existing member = %v", err)
	}
	if f, _ := v.Get("leo:famiglia.relazione"); f.Value != "figlio" {
		t.Fatalf("leo's relazione overwritten: %s", f.Value)
	}
}

func TestFormatHousehold(t *testing.T) {
	age, minor := 6, true
	h := &vault.Household{IntestatarioScheda: "io", Members: []vault.MemberInfo{
		{Slug: "giulia", RelazioneLabel: "moglie", FirstName: "Giulia", Famiglia: map[string]string{"stato_famiglia": "si"}, Fields: 3},
		{Slug: "leo", RelazioneLabel: "figlio/a", Age: &age, Minor: &minor, Famiglia: map[string]string{"genitori": "io,giulia", "a_carico_percentuale": "50"}, Fields: 4, Documents: 1},
	}}
	want := "giulia  moglie · Giulia · stato di famiglia\n" +
		"leo     figlio/a · 6 anni (minore) · a carico 50% · genitori io,giulia\n" +
		"Intestatario della scheda anagrafica: io\n"
	if got := formatHousehold(h, vault.KindPersona); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := formatHousehold(&vault.Household{}, vault.KindPersona); !strings.Contains(got, "No family members") {
		t.Fatalf("empty household: %q", got)
	}
	b := &vault.Household{Members: []vault.MemberInfo{
		{Slug: "bianchi", FirstName: "Anna", LastName: "Bianchi", Ruolo: map[string]string{"tipo": "titolare,legale_rappresentante", "quota_percentuale": "100"}},
		{Slug: "edil", Denominazione: "Edil SRL", Ruolo: map[string]string{"tipo": "impresa_esecutrice", "data_inizio": "2026-01-10"}},
	}}
	want = "bianchi  titolare, legale rappresentante · Anna Bianchi · quota 100%\n" +
		"edil     impresa esecutrice · Edil SRL · dal 2026-01-10\n"
	if got := formatHousehold(b, vault.KindImpresa); got != want {
		t.Fatalf("business people:\n%s\nwant:\n%s", got, want)
	}
	if got := formatHousehold(&vault.Household{}, vault.KindImpresa); !strings.Contains(got, "--ruolo") {
		t.Fatalf("empty business vault: %q", got)
	}
}

func TestExpiryLabel(t *testing.T) {
	if got := expiryLabel(vault.ExpiryInfo{Document: "cie", Person: "leo"}); got != "cie (leo)" {
		t.Errorf("member label = %q", got)
	}
	if got := expiryLabel(vault.ExpiryInfo{Document: "pratica tari"}); got != "pratica tari" {
		t.Errorf("owner label = %q", got)
	}
}

func TestFormatMigration(t *testing.T) {
	r := &vault.MigrationReport{DryRun: true,
		Members: []vault.MigratedMember{{N: 1, Slug: "giulia", Fields: map[string]string{"nucleo_familiare.membro1_nome": "giulia:identity.first_name"}}},
		Deleted: []string{"nucleo_familiare.numero_componenti"},
		Left:    map[string]string{"nucleo_familiare.membro1_scarpe": "no matching member field"},
	}
	got := formatMigration(r)
	for _, want := range []string{"membro1 → giulia", "Would move nucleo_familiare.membro1_nome → giulia:identity.first_name", "Drop nucleo_familiare.numero_componenti", "Left nucleo_familiare.membro1_scarpe", "Dry run"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// member remove needs one approval, worded for exactly that member, and no
// second prompt for full access.
func TestPersonRemove_OneScopedApproval(t *testing.T) {
	v, argsFile := touchIDVault(t, map[string]string{"leo:famiglia.relazione": "figlio", "leo:cie.numero": "CA12345AB"})
	needGrant("leo:*", "write")
	resp, err := apiRequest("DELETE", "/vault/members/leo", nil)
	if err != nil {
		t.Fatal(err)
	}
	var out vault.RemovedPerson
	if err := apiResult(resp, &out); err != nil || out.Fields != 2 {
		t.Fatalf("remove = %+v, %v", out, err)
	}
	if f, _ := v.FieldMeta("leo:cie.numero"); f != nil {
		t.Fatal("leo:cie.numero still there")
	}
	data, _ := os.ReadFile(argsFile)
	if want := "grant --scope leo:* --access write\n"; string(data) != want {
		t.Fatalf("helper calls:\n%s\nwant:\n%s", data, want)
	}
}
