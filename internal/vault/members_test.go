package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/crypto"
	"github.com/lovincyrus/personal-vault/internal/store"
)

func mustSet(t *testing.T, v *Vault, id, value string) {
	t.Helper()
	if err := v.Set(id, value, ""); err != nil {
		t.Fatalf("Set(%s): %v", id, err)
	}
}

// setLegacy stores a field the way an older version could, without Set's
// checks: IDs didn't have to be lower-case (Pratiche.x_stato). Unlock
// migrates such rows (relock).
func setLegacy(t *testing.T, v *Vault, id, value string) {
	t.Helper()
	setLegacyAt(t, v, id, value, time.Now())
}

func setLegacyAt(t *testing.T, v *Vault, id, value string, at time.Time) {
	t.Helper()
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(vaultKey)
	category, name, _ := strings.Cut(id, ".")
	subkey, err := crypto.DeriveSubkey(vaultKey, v.salt, category)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(subkey)
	encrypted, err := crypto.EncryptToBase64(subkey, []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.db.SetField(store.Field{ID: id, Category: category, FieldName: name, Value: encrypted,
		Sensitivity: DefaultSensitivity(id), UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
}

// relock locks the vault and unlocks it again, which runs the migrations.
func relock(t *testing.T, v *Vault) {
	t.Helper()
	sk, err := os.ReadFile(filepath.Join(v.dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if _, err := v.Unlock(testPassword, strings.TrimSpace(string(sk))); err != nil {
		t.Fatal(err)
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

	h, err := v.Household(nil, true, now)
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
	h, _ = v.Household(famigliaOnly, false, now)
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
	if h, _ := v.Household(ownerOnly, false, now); len(h.Members) != 0 {
		t.Errorf("owner scope saw members: %+v", h.Members)
	}
	cieOnly := func(id, tier string) bool { return ScopeAllows("leo:cie.*", id) }
	if h, _ := v.Household(cieOnly, false, now); len(h.Members) != 1 || h.Members[0].Famiglia != nil || h.IntestatarioScheda != "" {
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
	mustSet(t, v, "nucleo_familiare.numero_componenti", "4") // you and three membri
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
		"giulia-2:famiglia.stato_famiglia":   "si", // so the count stays 4
		"membro3:famiglia.stato_famiglia":    "si",
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

// A person is a record of their own: deleting their last field or document
// leaves them in the vault (only RemovePerson removes them, revoking what
// names them), and a service token can't name someone who isn't there yet.
func TestPersonOutlivesTheirRows(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:identity.first_name", "Leo")
	doc, err := v.AddDocument("leo:cie", "cie.pdf", "application/pdf", []byte("%PDF"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Delete("leo:identity.first_name"); err != nil {
		t.Fatal(err)
	}
	if ok, err := v.DeleteDocument(doc.ID); !ok || err != nil {
		t.Fatalf("delete leo's last document = %v, %v", ok, err)
	}
	if h, err := v.Household(nil, true, time.Now()); err != nil || len(h.Members) != 1 || h.Members[0].Slug != "leo" {
		t.Fatalf("household after leo's last row went = %+v, %v", h, err)
	}
	if h, _ := v.Household(func(string, string) bool { return true }, false, time.Now()); len(h.Members) != 0 {
		t.Fatalf("a token's household shows people it can't see a row of: %+v", h)
	}
	if out, err := v.ExportPerson("leo", false); err != nil || len(out.Fields)+len(out.Documents) != 0 {
		t.Fatalf("export of leo with no rows = %+v, %v; want an empty export", out, err)
	}

	if _, err := v.CreateServiceToken("x", "zed:*", time.Hour, "", ""); err == nil {
		t.Fatal("token naming a person who doesn't exist")
	}
	if _, err := v.CreateServiceToken("x", "*:cie.*", time.Hour, "", ""); err != nil {
		t.Fatalf("token for everyone's cie: %v", err)
	}
	if _, err := v.CreateServiceToken("x", "leo:*", time.Hour, "", ""); err != nil {
		t.Fatalf("token naming leo: %v", err)
	}
	removed, err := v.RemovePerson("leo")
	if err != nil || len(removed.RevokedTokens) != 1 {
		t.Fatalf("RemovePerson(leo) = %+v, %v", removed, err)
	}
	if _, err := v.RemovePerson("leo"); !errors.Is(err, ErrPersonNotFound) {
		t.Fatalf("second RemovePerson(leo) = %v, want ErrPersonNotFound", err)
	}
}

// When a document file can't be deleted, the person is still removed whole
// (tokens revoked, grants dropped) and the file is reported, not left for a
// retry that would find no person.
func TestRemovePerson_FileErrorStillRevokes(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:identity.first_name", "Leo")
	if _, err := v.AddDocument("leo:cie", "cie.pdf", "application/pdf", []byte("%PDF"), ""); err != nil {
		t.Fatal(err)
	}
	token, _ := v.CreateServiceToken("leo-agent", "leo:*", time.Hour, "", "")
	if _, _, err := v.session.AddGrant(time.Minute, "leo:*", "read"); err != nil {
		t.Fatal(err)
	}
	docsDir := filepath.Join(v.dir, "documents")
	if err := os.Chmod(docsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(docsDir, 0o700) })

	out, err := v.RemovePerson("leo")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.FilesLeft) != 1 || len(out.RevokedTokens) != 1 || out.DroppedGrants != 1 {
		t.Fatalf("removed = %+v", out)
	}
	if _, ok := v.ValidateServiceToken(token); ok {
		t.Error("leo:* token survived a removal whose file delete failed")
	}
	if fields, docs, _ := v.db.PersonRows("leo"); len(fields)+len(docs) != 0 {
		t.Fatalf("leo still has %v %v", fields, docs)
	}
}

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

// A numero_componenti the migrated members don't account for stays, so the
// household size the TARI tariff uses isn't silently changed.
func TestMigrateNucleo_KeepsMismatchedCount(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "nucleo_familiare.numero_componenti", "3")
	mustSet(t, v, "nucleo_familiare.membro1_nome", "Giulia")
	mustSet(t, v, "leo:identity.first_name", "Leo")
	mustSet(t, v, "leo:famiglia.stato_famiglia", "si")
	mustSet(t, v, "anna:identity.first_name", "Anna")
	mustSet(t, v, "anna:famiglia.stato_famiglia", "no")

	// You, leo and giulia make 3: the count is derived and goes.
	report, err := v.MigrateNucleo(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Deleted) != 1 || report.Left["nucleo_familiare.numero_componenti"] != "" {
		t.Fatalf("matching count: deleted %v, left %v", report.Deleted, report.Left)
	}

	mustSet(t, v, "nucleo_familiare.numero_componenti", "5")
	report, err = v.MigrateNucleo(false)
	if err != nil {
		t.Fatal(err)
	}
	if msg := report.Left["nucleo_familiare.numero_componenti"]; !strings.Contains(msg, "make 3") {
		t.Errorf("left = %v, want numero_componenti kept with the derived 3", report.Left)
	}
	if f, err := v.Get("nucleo_familiare.numero_componenti"); err != nil || f.Value != "5" {
		t.Errorf("numero_componenti = %+v, %v; want it kept", f, err)
	}
	if f, err := v.Get("giulia:famiglia.stato_famiglia"); err != nil || f.Value != "si" {
		t.Errorf("giulia:famiglia.stato_famiglia = %+v, %v; want si", f, err)
	}
}

// Field IDs from before they had to be lower-case are lower-case after
// unlocking, so the nucleo migration finds them.
func TestMigrateNucleo_MixedCase(t *testing.T) {
	v, _ := tmpVault(t)
	setLegacy(t, v, "Nucleo_Familiare.Membro1_Nome", "Anna")
	setLegacy(t, v, "nucleo_familiare.MEMBRO1_relazione", "Madre")
	relock(t, v)
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

// Migration checks person references like Set does: an intestatario_scheda
// naming nobody stays where it is (it would name whoever later gets the
// slug); one naming a member this migration adds moves.
func TestMigrateNucleo_PersonRefs(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "nucleo_familiare.intestatario_scheda", "marco")
	mustSet(t, v, "nucleo_familiare.membro1_nome", "Giulia")
	report, err := v.MigrateNucleo(false)
	if err != nil {
		t.Fatal(err)
	}
	if msg := report.Left["nucleo_familiare.intestatario_scheda"]; !strings.Contains(msg, `no person "marco"`) {
		t.Fatalf("left = %v, want intestatario_scheda kept for lack of marco", report.Left)
	}
	if f, _ := v.db.GetField("famiglia.intestatario_scheda"); f != nil {
		t.Fatal("famiglia.intestatario_scheda names a person who doesn't exist")
	}

	w, _ := tmpVault(t)
	mustSet(t, w, "nucleo_familiare.intestatario_scheda", "marco")
	mustSet(t, w, "nucleo_familiare.membro1_nome", "Marco")
	if report, err := w.MigrateNucleo(false); err != nil || report.Moved["nucleo_familiare.intestatario_scheda"] == "" {
		t.Fatalf("intestatario_scheda naming a migrated member: %+v, %v", report, err)
	}
}

// tokenFilter is what a service token with scope sees: its scope, and no
// personal tier.
func tokenFilter(scope string) FieldFilter {
	return func(id, tier string) bool { return ScopeAllows(scope, id) && tier != TierPersonal }
}

// A service token can write for people who exist, never create one: a slug
// taken by an agent would block 'pvault person add', and in a business vault
// skip the required --ruolo.
func TestServiceTokenCannotCreatePeople(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	allow := tokenFilter("*:cie.*")

	if err := v.SetChecked("leo:cie.numero", "CA12345AB", "", nil, allow); err != nil {
		t.Fatalf("token write for an existing person: %v", err)
	}
	var verr *ValidationError
	if err := v.SetChecked("newguy:cie.numero", "CA12345AB", "", nil, allow); !errors.As(err, &verr) {
		t.Fatalf("token write for a new person = %v, want ValidationError", err)
	}
	if _, err := v.AddDocumentIf(allow, "newguy:cie", "cie.pdf", "application/pdf", []byte("%PDF"), ""); !errors.As(err, &verr) {
		t.Fatalf("token document for a new person = %v, want ValidationError", err)
	}
	if ok, _ := v.db.PersonExists("newguy"); ok {
		t.Fatal("a token created a person")
	}

	// The session, and grants the user approved, still start people.
	mustSet(t, v, "newguy:cie.numero", "CA12345AB")
	if _, err := v.AddDocumentIf(nil, "other:cie", "cie.pdf", "application/pdf", []byte("%PDF"), ""); err != nil {
		t.Fatalf("session document for a new person: %v", err)
	}
}

// A closed pratica may outlive the person it names, but reopening it must not
// leave an open pratica naming nobody (or whoever takes the slug later).
func TestReopenPraticaNamingRemovedPerson(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "pratiche.cie-leo_persona", "leo")
	mustSet(t, v, "pratiche.cie-leo_stato", "completata")
	if _, err := v.RemovePerson("leo"); err != nil {
		t.Fatal(err)
	}
	var verr *ValidationError
	if err := v.Set("pratiche.cie-leo_stato", "in_attesa", ""); !errors.As(err, &verr) || !strings.Contains(err.Error(), "leo") {
		t.Fatalf("reopening = %v, want ValidationError naming leo", err)
	}
	if f, _ := v.Get("pratiche.cie-leo_stato"); f == nil || f.Value != "completata" {
		t.Fatalf("stato = %+v, want it still completata", f)
	}
	mustSet(t, v, "pratiche.cie-leo_stato", "respinta") // closed to closed is fine
	mustSet(t, v, "pratiche.cie-leo_persona", "io")
	mustSet(t, v, "pratiche.cie-leo_stato", "in_attesa")
}

// A token's view of the household never shows a person it can't see a row of.
func TestHouseholdRedactsHiddenPeople(t *testing.T) {
	v, _ := tmpVault(t)
	if err := v.Set("zed:cie.numero", "CA12345AB", TierPersonal); err != nil { // hidden from tokens
		t.Fatal(err)
	}
	mustSet(t, v, "famiglia.intestatario_scheda", "zed")
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "leo:famiglia.genitori", "io,zed")

	h, err := v.Household(tokenFilter("famiglia.*,*:famiglia.*"), false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if h.IntestatarioScheda != "" {
		t.Errorf("intestatario_scheda = %q, want it hidden", h.IntestatarioScheda)
	}
	if len(h.Members) != 1 || h.Members[0].Famiglia["genitori"] != "io" {
		t.Errorf("members = %+v, want only leo with genitori io", h.Members)
	}

	h, err = v.Household(nil, true, time.Now())
	if err != nil || h.IntestatarioScheda != "zed" {
		t.Fatalf("session view = %+v, %v", h, err)
	}
}

// Every way of reopening a pratica checks its persona: a token that can't read
// persona, and deleting the stato (a pratica without one counts as open).
func TestReopenPraticaChecksPersonaWhateverTheRoute(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "pratiche.cie-leo_persona", "leo")
	mustSet(t, v, "pratiche.cie-leo_stato", "completata")
	if _, err := v.RemovePerson("leo"); err != nil {
		t.Fatal(err)
	}

	narrow := tokenFilter("pratiche.cie-leo_stato") // can't read persona
	err := v.SetChecked("pratiche.cie-leo_stato", "in_attesa", "", &FieldInfo{Sensitivity: "standard"}, narrow)
	var verr *ValidationError
	if !errors.As(err, &verr) || strings.Contains(err.Error(), `"leo"`) {
		t.Fatalf("token reopening = %v, want a refusal that doesn't name leo", err)
	}
	if err := v.Delete("pratiche.cie-leo_stato"); !errors.As(err, &verr) {
		t.Fatalf("deleting the stato = %v, want ValidationError", err)
	}
	if err := v.DeleteChecked("pratiche.cie-leo_stato", &FieldInfo{Sensitivity: "standard"}); !errors.As(err, &verr) || strings.Contains(err.Error(), `"leo"`) {
		t.Fatalf("token deleting the stato = %v", err)
	}
	if f, _ := v.Get("pratiche.cie-leo_stato"); f == nil || f.Value != "completata" {
		t.Fatalf("stato = %+v, want it still completata", f)
	}
	// Once persona is fixed, either route works.
	mustSet(t, v, "pratiche.cie-leo_persona", "io")
	if err := v.Delete("pratiche.cie-leo_stato"); err != nil {
		t.Fatalf("delete after fixing persona: %v", err)
	}
}

// A slug a closed pratica still names stays the removed person's: a new
// person can't take it (and so inherit the pratica in expiring, their export,
// or by reopening it), until those pratiche are gone.
func TestNewPersonCantTakeSlugAPraticaNames(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "pratiche.cie-leo_persona", "leo")
	mustSet(t, v, "pratiche.cie-leo_stato", "completata")
	if _, err := v.RemovePerson("leo"); err != nil {
		t.Fatal(err)
	}
	var verr *ValidationError
	if err := v.Set("leo:famiglia.relazione", "altro", ""); !errors.As(err, &verr) || !strings.Contains(err.Error(), `1 pratica(s) still name "leo"`) || strings.Contains(err.Error(), "cie-leo") {
		t.Fatalf("new person with the slug = %v, want a refusal that counts the pratiche without naming them", err)
	}
	// Deleting the pratica starts with its persona: the stato alone would reopen it.
	if err := v.Delete("pratiche.cie-leo_stato"); err == nil || !strings.Contains(err.Error(), "delete its persona field first") {
		t.Fatalf("deleting the stato first = %v, want advice to delete persona first", err)
	}
	if _, err := v.AddDocument("leo:cie", "cie.pdf", "application/pdf", []byte("scan"), ""); !errors.As(err, &verr) {
		t.Fatalf("document for a new person with the slug = %v, want ValidationError", err)
	}
	if exists, _ := v.db.PersonExists("leo"); exists {
		t.Fatal("leo was added")
	}

	// The nucleo migration picks another slug for a membro of that name.
	mustSet(t, v, "nucleo_familiare.membro1_nome", "Leo")
	report, err := v.MigrateNucleo(false)
	if err != nil || len(report.Members) != 1 || report.Members[0].Slug != "leo-2" {
		t.Fatalf("migration = %+v, %v; want membro1 as leo-2", report, err)
	}

	// Once the pratica is deleted, the slug is free.
	for _, id := range []string{"pratiche.cie-leo_persona", "pratiche.cie-leo_stato"} { // persona first: no stato is open
		if err := v.Delete(id); err != nil {
			t.Fatal(err)
		}
	}
	mustSet(t, v, "leo:famiglia.relazione", "altro")
}

// A second run of the nucleo migration (after fixing what the first left)
// adds the leftover fields to the person the first run made, not to a new
// one named membro<N>; removing that person forgets it.
func TestMigrateNucleo_RerunJoinsSamePerson(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "nucleo_familiare.membro1_nome", "Giulia")
	mustSet(t, v, "nucleo_familiare.membro1_relazione", "zia acquisita") // not a relazione: left
	if _, err := v.MigrateNucleo(false); err != nil {
		t.Fatal(err)
	}
	mustSet(t, v, "nucleo_familiare.membro1_relazione", "altro_affine")
	report, err := v.MigrateNucleo(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Members) != 1 || report.Members[0].Slug != "giulia" {
		t.Fatalf("second run members = %+v, want giulia", report.Members)
	}
	if slugs, _ := v.db.PersonSlugs(); strings.Join(slugs, ",") != "giulia" {
		t.Fatalf("people = %v, want only giulia", slugs)
	}
	if f, err := v.Get("giulia:famiglia.relazione"); err != nil || f.Value != "altro_affine" {
		t.Fatalf("giulia:famiglia.relazione = %+v, %v", f, err)
	}

	// Nothing of membro1 is left, so the record goes: someone entered in
	// that slot later is a new person, never giulia.
	if m, _ := v.db.MetaWithPrefix(metaNucleoMembro); len(m) != 0 {
		t.Errorf("migration record kept after membro1 moved whole: %v", m)
	}
	mustSet(t, v, "nucleo_familiare.membro1_cognome", "Rossi")
	mustSet(t, v, "nucleo_familiare.membro1_relazione", "figlio")
	if report, err := v.MigrateNucleo(false); err != nil || len(report.Members) != 1 || report.Members[0].Slug != "membro1" {
		t.Fatalf("third run = %+v, %v; want a new person", report, err)
	}
	if f, _ := v.Get("giulia:famiglia.relazione"); f == nil || f.Value != "altro_affine" {
		t.Errorf("giulia:famiglia.relazione = %+v, want it untouched", f)
	}
}

// A new member entered under a membro<N> whose earlier person still has
// fields left behind is a new person: their nome says so.
func TestMigrateNucleo_NewNomeIsNewPerson(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "nucleo_familiare.membro1_nome", "Giulia")
	mustSet(t, v, "nucleo_familiare.membro1_relazione", "zia acquisita") // left: keeps the record
	if _, err := v.MigrateNucleo(false); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete("nucleo_familiare.membro1_relazione"); err != nil {
		t.Fatal(err)
	}
	mustSet(t, v, "nucleo_familiare.membro1_nome", "Marco")
	mustSet(t, v, "nucleo_familiare.membro1_sesso", "M")
	report, err := v.MigrateNucleo(false)
	if err != nil || len(report.Members) != 1 || report.Members[0].Slug != "marco" {
		t.Fatalf("second run = %+v, %v; want marco", report, err)
	}
	if f, _ := v.Get("giulia:identity.sex"); f != nil {
		t.Errorf("Marco's sex landed on giulia: %+v", f)
	}
}

// Removing the person a membro became forgets the record, so a later person
// with that slug never gets the membro's leftover fields.
func TestRemovePerson_ForgetsNucleoRecord(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "nucleo_familiare.membro1_nome", "Giulia")
	mustSet(t, v, "nucleo_familiare.membro1_relazione", "zia acquisita")
	if _, err := v.MigrateNucleo(false); err != nil {
		t.Fatal(err)
	}
	if m, _ := v.db.MetaWithPrefix(metaNucleoMembro); m[metaNucleoMembro+"1"] != "giulia" {
		t.Fatalf("record = %v, want membro1 → giulia", m)
	}
	if _, err := v.RemovePerson("giulia"); err != nil {
		t.Fatal(err)
	}
	if m, _ := v.db.MetaWithPrefix(metaNucleoMembro); len(m) != 0 {
		t.Errorf("record kept after removing giulia: %v", m)
	}
}
