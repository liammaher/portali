package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustSet(t *testing.T, v *Vault, id, value string) {
	t.Helper()
	if err := v.Set(id, value, ""); err != nil {
		t.Fatalf("Set(%s): %v", id, err)
	}
}

func TestHousehold(t *testing.T) {
	v, _ := tmpVault(t)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	mustSet(t, v, "giulia:famiglia.relazione", "moglie")
	mustSet(t, v, "giulia:identity.first_name", "Giulia")
	mustSet(t, v, "giulia:identity.sex", "F")
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "leo:famiglia.genitori", "io,giulia")
	mustSet(t, v, "leo:identity.date_of_birth", "2019-10-27") // turns 7 tomorrow
	mustSet(t, v, "leo:cie.numero", "CA12345AB")
	mustSet(t, v, "famiglia.intestatario_scheda", "io")
	if _, err := v.AddDocument("leo:cie", "cie.pdf", "application/pdf", []byte("%PDF"), ""); err != nil {
		t.Fatal(err)
	}

	h, err := v.Household(nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if h.IntestatarioScheda != "io" || len(h.Members) != 2 {
		t.Fatalf("household = %+v", h)
	}
	giulia, leo := h.Members[0], h.Members[1]
	if giulia.Slug != "giulia" || giulia.RelazioneLabel != "moglie" || giulia.ANPRRelazione != "2" || giulia.FirstName != "Giulia" || giulia.Age != nil {
		t.Errorf("giulia = %+v", giulia)
	}
	if leo.Slug != "leo" || leo.Age == nil || *leo.Age != 6 || !*leo.Minor || leo.RelazioneLabel != "figlio/a" ||
		leo.Famiglia["genitori"] != "io,giulia" || leo.Fields != 4 || leo.Documents != 1 {
		t.Errorf("leo = %+v", leo)
	}

	// A filter sees only what it allows, and derived values only from
	// fields it can read.
	famigliaOnly := func(id, tier string) bool { return ScopeAllows("*:famiglia.*", id) && TierAllows("sensitive", tier) }
	h, _ = v.Household(famigliaOnly, now)
	if h.IntestatarioScheda != "io" || len(h.Members) != 2 {
		t.Fatalf("famiglia-only household = %+v", h)
	}
	if g := h.Members[0]; g.FirstName != "" || g.RelazioneLabel != "coniuge" || g.Fields != 1 || g.Documents != 0 {
		t.Errorf("famiglia-only giulia = %+v (no name, neutral label, one field)", g)
	}
	if l := h.Members[1]; l.Age != nil || l.Minor != nil || l.Documents != 0 {
		t.Errorf("famiglia-only leo = %+v (no age without date_of_birth)", l)
	}
	ownerOnly := func(id, tier string) bool { return ScopeAllows("identity.*,famiglia.*", id) }
	if h, _ := v.Household(ownerOnly, now); len(h.Members) != 0 {
		t.Errorf("owner scope saw members: %+v", h.Members)
	}
	cieOnly := func(id, tier string) bool { return ScopeAllows("leo:cie.*", id) }
	if h, _ := v.Household(cieOnly, now); len(h.Members) != 1 || h.Members[0].Famiglia != nil || h.IntestatarioScheda != "" {
		t.Errorf("leo:cie.* household = %+v", h)
	}
}

func TestRemovePerson(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "giulia:famiglia.relazione", "moglie")
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "leo:famiglia.genitori", "io,giulia")
	mustSet(t, v, "leo:spid.password", "hunter2-secret")
	mustSet(t, v, "pratiche.cie-leo_persona", "leo")
	doc, err := v.AddDocument("leo:cie", "cie.pdf", "application/pdf", []byte("%PDF"), "")
	if err != nil {
		t.Fatal(err)
	}
	leoToken, _ := v.CreateServiceToken("leo-agent", "leo:cie.*", time.Hour, "", "")
	ownerToken, _ := v.CreateServiceToken("owner-agent", "identity.*,*:identity.*", time.Hour, "", "")
	grant, _, err := v.session.AddGrant(time.Minute, "leo:*", "read")
	if err != nil {
		t.Fatal(err)
	}

	// Named by leo's genitori: giulia can't go yet.
	var inUse *PersonInUseError
	if _, err := v.RemovePerson("giulia"); !errors.As(err, &inUse) || inUse.Refs[0] != "leo:famiglia.genitori" {
		t.Fatalf("remove giulia = %v, want PersonInUseError naming leo:famiglia.genitori", err)
	}
	if _, err := v.RemovePerson("leo"); !errors.As(err, &inUse) || inUse.Refs[0] != "pratiche.cie-leo_persona" {
		t.Fatalf("remove leo = %v, want PersonInUseError naming the pratica", err)
	}
	mustSet(t, v, "pratiche.cie-leo_persona", "io")

	out, err := v.RemovePerson("leo")
	if err != nil {
		t.Fatal(err)
	}
	if out.Fields != 3 || out.Documents != 1 || strings.Join(out.RevokedTokens, ",") != "leo-agent" || out.DroppedGrants != 1 {
		t.Fatalf("removed = %+v", out)
	}
	if fields, docs, _ := v.db.PersonRows("leo"); len(fields)+len(docs) != 0 {
		t.Fatalf("leo still has %v %v", fields, docs)
	}
	if _, err := os.Stat(v.documentPath(doc.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("document file still there: %v", err)
	}
	if _, ok := v.ValidateServiceToken(leoToken); ok {
		t.Error("leo:cie.* token survived")
	}
	if _, ok := v.ValidateServiceToken(ownerToken); !ok {
		t.Error("a token not naming leo was revoked")
	}
	if _, _, ok := v.ScopedGrant(grant); ok {
		t.Error("leo:* grant survived")
	}
	if _, err := v.RemovePerson("leo"); err != ErrPersonNotFound {
		t.Fatalf("second remove = %v, want ErrPersonNotFound", err)
	}
	// giulia is no longer named, so she can go now.
	if _, err := v.RemovePerson("giulia"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.RemovePerson("io"); err == nil {
		t.Fatal("removed the owner")
	}
}

func TestMigrateNucleo(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "nucleo_familiare.numero_componenti", "3")
	mustSet(t, v, "nucleo_familiare.intestatario_scheda", "si")
	mustSet(t, v, "nucleo_familiare.membro1_nome", "Giulia")
	mustSet(t, v, "nucleo_familiare.membro1_cognome", "Bianchi")
	mustSet(t, v, "nucleo_familiare.membro1_relazione", "moglie")
	mustSet(t, v, "nucleo_familiare.membro1_data_nascita", "15/03/1990")
	mustSet(t, v, "nucleo_familiare.membro2_nome", "Leò")
	mustSet(t, v, "nucleo_familiare.membro2_relazione", "amico del cuore") // not a relazione
	mustSet(t, v, "nucleo_familiare.membro2_scarpe", "32")                 // no such member field
	mustSet(t, v, "nucleo_familiare.membro3_codice_fiscale", "RSSMRA85T10A562S")
	mustSet(t, v, "giulia:identity.first_name", "Giulia") // already a member: membro1 becomes giulia-2

	report, err := v.MigrateNucleo(true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || len(report.Members) != 3 {
		t.Fatalf("dry run = %+v", report)
	}
	if f, _ := v.db.GetField("nucleo_familiare.membro1_nome"); f == nil {
		t.Fatal("dry run moved a field")
	}

	report, err = v.MigrateNucleo(false)
	if err != nil {
		t.Fatal(err)
	}
	slugs := []string{}
	for _, m := range report.Members {
		slugs = append(slugs, m.Slug)
	}
	if strings.Join(slugs, ",") != "giulia-2,leo,membro3" {
		t.Fatalf("slugs = %v", slugs)
	}
	for id, want := range map[string]string{
		"giulia-2:identity.first_name":       "Giulia",
		"giulia-2:famiglia.relazione":        "coniuge",
		"giulia-2:identity.date_of_birth":    "1990-03-15",
		"leo:identity.first_name":            "Leò",
		"membro3:identity.codice_fiscale":    "RSSMRA85T10A562S",
		"famiglia.intestatario_scheda":       "io",
		"nucleo_familiare.membro2_relazione": "amico del cuore", // left in place
		"nucleo_familiare.membro2_scarpe":    "32",
	} {
		f, err := v.Get(id)
		if err != nil || f.Value != want {
			t.Errorf("%s = %+v, %v; want %q", id, f, err, want)
		}
	}
	for _, id := range []string{"nucleo_familiare.membro1_nome", "nucleo_familiare.numero_componenti", "nucleo_familiare.intestatario_scheda"} {
		if f, _ := v.db.GetField(id); f != nil {
			t.Errorf("%s still there", id)
		}
	}
	if len(report.Left) != 2 || report.Left["nucleo_familiare.membro2_relazione"] == "" {
		t.Errorf("left = %v", report.Left)
	}
	// Tiers are never lowered: data_nascita was standard, date_of_birth is sensitive.
	if f, _ := v.FieldMeta("giulia-2:identity.date_of_birth"); f.Sensitivity != "sensitive" {
		t.Errorf("date_of_birth tier = %s, want sensitive", f.Sensitivity)
	}
	if f, _ := v.FieldMeta("membro3:identity.codice_fiscale"); f.Sensitivity != "sensitive" {
		t.Errorf("codice_fiscale tier = %s", f.Sensitivity)
	}
}

func TestSlugFromName(t *testing.T) {
	for in, want := range map[string]string{
		"Giulia": "giulia", "Maria Luisa": "maria-luisa", "Niccolò": "niccolo", "D'Amico": "d-amico",
		" Anna-Maria ": "anna-maria", "123": "", "Io": "", "": "", "J.R.": "jr", "a - b": "a-b", "1anna": "anna",
	} {
		if got := slugFromName(in); got != want {
			t.Errorf("slugFromName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A backup carries members' fields and documents, each under its own keys.
func TestBackupRestoresMembers(t *testing.T) {
	v, sk, _ := backupVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "leo:cie.numero", "CA12345AB")
	doc, err := v.AddDocument("leo:cie", "cie.pdf", "application/pdf", []byte("leo's card"), "")
	if err != nil {
		t.Fatal(err)
	}
	path, err := v.Backup()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "restored")
	check, err := RestoreBackup(path, dir, testPassword, sk)
	if err != nil {
		t.Fatal(err)
	}
	if check.Fields != 4 || check.Documents != 2 {
		t.Fatalf("restore counted %+v, want members' rows too", check)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	if f, err := r.Get("leo:cie.numero"); err != nil || f.Value != "CA12345AB" {
		t.Fatalf("restored leo:cie.numero = %+v, %v", f, err)
	}
	if info, data, err := r.GetDocument(doc.ID); err != nil || string(data) != "leo's card" || info.Category != "leo:cie" {
		t.Fatalf("restored document = %+v %q %v", info, data, err)
	}
}

// A closed pratica is history: it names a member without keeping them.
func TestRemovePerson_ClosedPraticaDoesNotBlock(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "pratiche.cie-leo_persona", "leo")
	mustSet(t, v, "pratiche.cie-leo_stato", "in_attesa")
	var inUse *PersonInUseError
	if _, err := v.RemovePerson("leo"); !errors.As(err, &inUse) {
		t.Fatalf("open pratica: remove = %v, want PersonInUseError", err)
	}
	mustSet(t, v, "pratiche.cie-leo_stato", "completata")
	if _, err := v.RemovePerson("leo"); err != nil {
		t.Fatalf("closed pratica: remove = %v", err)
	}
}

// Field IDs ignore case elsewhere, so migration does too.
func TestMigrateNucleo_MixedCase(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "Nucleo_Familiare.Membro1_Nome", "Anna")
	mustSet(t, v, "nucleo_familiare.MEMBRO1_relazione", "Madre")
	report, err := v.MigrateNucleo(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Members) != 1 || report.Members[0].Slug != "anna" || len(report.Left) != 0 {
		t.Fatalf("report = %+v", report)
	}
	if f, err := v.Get("anna:famiglia.relazione"); err != nil || f.Value != "genitore" {
		t.Fatalf("anna:famiglia.relazione = %+v, %v", f, err)
	}
}
