package vault

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func fileSource(t *testing.T, v *Vault) string {
	t.Helper()
	src, err := v.AddFileSource("local", FileSourceConfig{Label: "OneDrive Acme", Root: "/Users/x/Library/CloudStorage/OneDrive-Acme"})
	if err != nil {
		t.Fatal(err)
	}
	return src.ID
}

func obs(key, path, name string) FileObservation {
	return FileObservation{Key: key, Path: path, Name: name, MIME: "application/pdf", Size: 10, Mtime: "2026-10-01T10:00:00Z"}
}

func TestFiles_EncryptedAtRestAndSearch(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	res, err := v.UpsertFiles(src, []FileObservation{
		obs("/Clienti/Rossi/Visura Camerale.pdf", "/Clienti/Rossi/Visura Camerale.pdf", "Visura Camerale.pdf"),
		obs("/Clienti/Bianchi/Contratto.pdf", "/Clienti/Bianchi/Contratto.pdf", "Contratto.pdf"),
	}, true)
	if err != nil || res.Added != 2 {
		t.Fatalf("upsert = %+v, %v", res, err)
	}

	// Nothing identifying is in a plain column or in the file.
	rows, _ := v.db.ListFiles("")
	for _, r := range rows {
		if strings.Contains(r.Blob, "Rossi") || strings.Contains(r.Blob, "Visura") {
			t.Fatal("file blob in plaintext")
		}
	}
	hits, total, err := v.SearchFiles(nil, FileQuery{Text: "visura rossi"})
	if err != nil || total != 1 || hits[0].Name != "Visura Camerale.pdf" {
		t.Fatalf("search = %+v, %d, %v", hits, total, err)
	}
	// Accents and case are ignored.
	if _, total, _ := v.SearchFiles(nil, FileQuery{Text: "CAMERALÈ"}); total != 1 {
		t.Fatalf("accent-folded search = %d hits", total)
	}
}

func TestFiles_BlobBoundToRow(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("a", "/a.pdf", "a.pdf"), obs("b", "/b.pdf", "b.pdf")}, true)
	rows, _ := v.db.ListFiles("")
	a, b := rows[0], rows[1]
	b.Blob = a.Blob
	if err := v.db.UpdateFile(b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.SearchFiles(nil, FileQuery{}); err == nil {
		t.Fatal("a blob copied onto another row should fail to decrypt")
	}
}

func TestFiles_ResyncKeepsLinksAcrossRenameAndTombstonesNeverDeletes(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("id-1", "/Old/visura.pdf", "visura.pdf")}, true)
	hits, _, _ := v.SearchFiles(nil, FileQuery{})
	if _, err := v.LinkFile(nil, hits[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson, Role: "visura"}, FileOriginManual); err != nil {
		t.Fatal(err)
	}

	// Same key, new name and path: the same file, still linked.
	res, _ := v.UpsertFiles(src, []FileObservation{obs("id-1", "/New/visura 2026.pdf", "visura 2026.pdf")}, true)
	if res.Updated != 1 || res.Added != 0 {
		t.Fatalf("rename = %+v", res)
	}
	hits, _, _ = v.SearchFiles(nil, FileQuery{Text: "2026"})
	if len(hits) != 1 || len(hits[0].Links) != 1 || hits[0].Links[0].Role != "visura" {
		t.Fatalf("after rename: %+v", hits)
	}

	// Missing from a full listing: tombstoned, linked row kept.
	res, _ = v.UpsertFiles(src, nil, true)
	if res.Gone != 1 {
		t.Fatalf("vanish = %+v", res)
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{}); total != 0 {
		t.Fatal("a gone file should not be listed by default")
	}
	gone, _, _ := v.SearchFiles(nil, FileQuery{IncludeGone: true})
	if len(gone) != 1 || !gone[0].Gone || len(gone[0].Links) != 1 {
		t.Fatalf("gone file = %+v", gone)
	}
	if n, _ := v.PruneFiles(); n != 0 {
		t.Fatal("prune must keep a file that has a link")
	}

	// It comes back when seen again.
	v.UpsertFiles(src, []FileObservation{obs("id-1", "/New/visura 2026.pdf", "visura 2026.pdf")}, true)
	if _, total, _ := v.SearchFiles(nil, FileQuery{}); total != 1 {
		t.Fatal("a file seen again should return")
	}
}

func TestFiles_PruneDropsOnlyUnlinkedTombstones(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("a", "/a.pdf", "a.pdf"), obs("b", "/b.pdf", "b.pdf")}, true)
	v.UpsertFiles(src, nil, true)
	if n, err := v.PruneFiles(); err != nil || n != 2 {
		t.Fatalf("prune = %d, %v", n, err)
	}
	if rows, _ := v.db.ListFiles(""); len(rows) != 0 {
		t.Fatal("pruned files remain")
	}
}

func TestFiles_TokenScopeAndTiers(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{
		obs("1", "/Casa/contratto.pdf", "contratto.pdf"),
		obs("2", "/Leo/scuola.pdf", "scuola.pdf"),
		obs("3", "/Leo/SPID password.txt", "SPID password.txt"),
	}, true)
	all, _, _ := v.SearchFiles(nil, FileQuery{})
	var scuola, spid FileInfo
	for _, f := range all {
		switch f.Name {
		case "scuola.pdf":
			scuola = f
		case "SPID password.txt":
			spid = f
		}
	}
	if spid.Tier != TierPersonal {
		t.Fatalf("a file named for a login secret should be personal, got %q", spid.Tier)
	}
	if _, err := v.LinkFile(nil, scuola.ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, FileOriginManual); err != nil {
		t.Fatal(err)
	}

	owner := tokenFilter("files.*")
	leo := tokenFilter("leo:files.*")

	names := func(allow FieldFilter) []string {
		hits, _, err := v.SearchFiles(allow, FileQuery{})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, h := range hits {
			out = append(out, h.Name)
		}
		return out
	}
	// The owner scope sees unlinked files but not Leo's, and never the personal one.
	if got := strings.Join(names(owner), ","); got != "contratto.pdf" {
		t.Fatalf("owner token sees %q", got)
	}
	if got := strings.Join(names(leo), ","); got != "scuola.pdf" {
		t.Fatalf("leo token sees %q", got)
	}
	if _, err := v.GetFileInfo(owner, scuola.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("a file outside the scope = %v, want not found", err)
	}
	if _, err := v.GetFileInfo(leo, spid.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("a personal file for a token = %v, want not found", err)
	}
	if _, err := v.GetFileInfo(nil, spid.ID); err != nil {
		t.Fatalf("the session reads a personal file: %v", err)
	}
}

func TestFiles_LinkRules(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("1", "/a.pdf", "a.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{})

	var verr *ValidationError
	for _, in := range []FileLinkInput{
		{Holder: "ghost", Kind: FileKindPerson},                 // no such person
		{Holder: "", Kind: FileKindClient},                      // a client is a person
		{Holder: "leo", Kind: FileKindPratica, Pratica: "x"},    // a pratica is the owner's
		{Holder: "", Kind: FileKindPratica, Pratica: "nessuna"}, // no such pratica
		{Holder: "", Kind: FileKindBusiness},                    // only in a business vault
		{Holder: "leo", Kind: "boh"},
	} {
		if _, err := v.LinkFile(nil, f[0].ID, in, FileOriginManual); !errors.As(err, &verr) {
			t.Errorf("LinkFile(%+v) = %v, want ValidationError", in, err)
		}
	}

	// A real pratica and a real person link; linking again returns the same link.
	mustSet(t, v, "pratiche.tari-2026_stato", "in_preparazione")
	l1, err := v.LinkFile(nil, f[0].ID, FileLinkInput{Kind: FileKindPratica, Pratica: "tari-2026"}, FileOriginManual)
	if err != nil {
		t.Fatal(err)
	}
	l2, err := v.LinkFile(nil, f[0].ID, FileLinkInput{Kind: FileKindPratica, Pratica: "tari-2026"}, FileOriginManual)
	if err != nil || l1.ID != l2.ID {
		t.Fatalf("second link = %+v, %v", l2, err)
	}
	pr := "tari-2026"
	hits, _, _ := v.SearchFiles(nil, FileQuery{Pratica: pr})
	if len(hits) != 1 {
		t.Fatalf("search by pratica = %d", len(hits))
	}
	if err := v.UnlinkFile(nil, l1.ID); err != nil {
		t.Fatal(err)
	}
	if err := v.UnlinkFile(nil, l1.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("unlink twice = %v", err)
	}
}

func TestFiles_BusinessVaultKinds(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("1", "/a.pdf", "a.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{})
	if _, err := v.LinkFile(nil, f[0].ID, FileLinkInput{Kind: FileKindBusiness}, FileOriginManual); err != nil {
		t.Fatalf("business link: %v", err)
	}
	var verr *ValidationError
	if _, err := v.LinkFile(nil, f[0].ID, FileLinkInput{Kind: FileKindPerson}, FileOriginManual); !errors.As(err, &verr) {
		t.Fatalf("a person link to nobody in a business vault = %v", err)
	}
}

func TestFiles_FolderRulesLinkNowAndLater(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{
		obs("1", "/Clienti/Rossi/a.pdf", "a.pdf"),
		obs("2", "/Clienti/Rossini/b.pdf", "b.pdf"), // not below /Clienti/Rossi
	}, true)

	rule, n, err := v.AddFileRule(nil, src, FileRuleInput{Path: "clienti/rossi", Holder: "leo", Kind: FileKindPerson})
	if err != nil || n != 1 || rule.Path != "/clienti/rossi" {
		t.Fatalf("rule = %+v, linked %d, %v", rule, n, err)
	}
	// A file indexed later under the folder is linked on arrival.
	res, _ := v.UpsertFiles(src, []FileObservation{
		obs("1", "/Clienti/Rossi/a.pdf", "a.pdf"),
		obs("2", "/Clienti/Rossini/b.pdf", "b.pdf"),
		obs("3", "/Clienti/Rossi/Sub/c.pdf", "c.pdf"),
	}, true)
	if res.Added != 1 || res.Linked != 1 {
		t.Fatalf("later file = %+v", res)
	}
	holder := "leo"
	hits, _, _ := v.SearchFiles(nil, FileQuery{Holder: &holder})
	if len(hits) != 2 {
		t.Fatalf("leo's files = %d, want 2", len(hits))
	}
	for _, h := range hits {
		if h.Links[0].Origin != FileOriginRule {
			t.Fatalf("origin = %q", h.Links[0].Origin)
		}
	}
	if unl, _, _ := v.SearchFiles(nil, FileQuery{Unlinked: true}); len(unl) != 1 || unl[0].Name != "b.pdf" {
		t.Fatalf("unlinked = %+v", unl)
	}
}

func TestFiles_SuggestionsNeedTheUser(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("1", "/a.pdf", "a.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{})
	agent := tokenFilter("*:files.*")

	s, err := v.SuggestFileLink(agent, f[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, "agent-1", "name says Leo")
	if err != nil {
		t.Fatal(err)
	}
	// Still unlinked until accepted.
	if unl, _, _ := v.SearchFiles(nil, FileQuery{Unlinked: true}); len(unl) != 1 {
		t.Fatal("a suggestion must not link")
	}
	link, err := v.ResolveFileSuggestion(nil, s.ID, true)
	if err != nil || link.Origin != FileOriginSuggestion || link.Note != "name says Leo" {
		t.Fatalf("accept = %+v, %v", link, err)
	}
	if left, _ := v.FileSuggestions(nil); len(left) != 0 {
		t.Fatal("accepted suggestion still listed")
	}

	// A token can't suggest for a holder it can't reach, or about a file it can't see.
	narrow := tokenFilter("files.*")
	if _, err := v.SuggestFileLink(narrow, f[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, "agent-2", ""); err == nil {
		t.Fatal("suggestion for a holder outside the token's scope")
	}
	s2, _ := v.SuggestFileLink(agent, f[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindClient}, "agent-1", "")
	if _, err := v.ResolveFileSuggestion(nil, s2.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ResolveFileSuggestion(nil, s2.ID, false); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("resolve twice = %v", err)
	}
}

func TestFiles_NeverStoreSharingLinks(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	for _, u := range []string{
		"https://1drv.ms/b/s!AbCdEf",
		"https://acme.sharepoint.com/:b:/g/personal/x/EabC",
		"https://www.dropbox.com/s/abc123/file.pdf?dl=0",
		"https://www.dropbox.com/scl/fi/abc/file.pdf?rlkey=xyz",
		"https://drive.google.com/file/d/ID/view?usp=sharing",
		"http://acme.sharepoint.com/sites/x/doc.pdf",
		"https://user:pw@acme.sharepoint.com/x",
		"https://acme.sharepoint.com/sites/x/doc.pdf?e=AbC",
	} {
		o := obs("k", "/a.pdf", "a.pdf")
		o.WebURL = u
		var verr *ValidationError
		if _, err := v.UpsertFiles(src, []FileObservation{o}, false); !errors.As(err, &verr) {
			t.Errorf("%s accepted (err = %v)", u, err)
		}
	}
	o := obs("k", "/a.pdf", "a.pdf")
	o.WebURL = "https://acme.sharepoint.com/sites/x/Shared%20Documents/a.pdf"
	if _, err := v.UpsertFiles(src, []FileObservation{o}, false); err != nil {
		t.Fatalf("a plain item URL refused: %v", err)
	}
}

func TestFiles_RemovePersonDropsTheirLinksButNotTheIndex(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("1", "/Leo/a.pdf", "a.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{})
	v.LinkFile(nil, f[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, FileOriginManual)
	v.AddFileRule(nil, src, FileRuleInput{Path: "/Leo", Holder: "leo", Kind: FileKindPerson})

	out, err := v.RemovePerson("leo")
	if err != nil {
		t.Fatal(err)
	}
	if out.FileLinks != 2 {
		t.Fatalf("removal reported %d file links, want 2 (link + rule)", out.FileLinks)
	}
	if rules, _ := v.FileRules(nil, ""); len(rules) != 0 {
		t.Fatal("rule for a removed person remains")
	}
	hits, _, _ := v.SearchFiles(nil, FileQuery{})
	if len(hits) != 1 || len(hits[0].Links) != 0 {
		t.Fatalf("file after removal = %+v", hits)
	}
	// A new person with the same slug inherits nothing.
	mustSet(t, v, "leo:famiglia.relazione", "nipote")
	if hits, _, _ := v.SearchFiles(nil, FileQuery{}); len(hits[0].Links) != 0 {
		t.Fatal("a new leo inherited a link")
	}
}

func TestFiles_ForgetSourceErasesItsRows(t *testing.T) {
	v, _ := tmpVault(t)
	a, b := fileSource(t, v), fileSource(t, v)
	v.UpsertFiles(a, []FileObservation{obs("1", "/a.pdf", "a.pdf")}, true)
	v.UpsertFiles(b, []FileObservation{obs("1", "/b.pdf", "b.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{SourceID: a})
	v.LinkFile(nil, f[0].ID, FileLinkInput{Kind: FileKindPerson}, FileOriginManual)
	v.AddFileRule(nil, a, FileRuleInput{Path: "/", Kind: FileKindPerson})

	n, err := v.ForgetFileSource(a)
	if err != nil || n != 1 {
		t.Fatalf("forget = %d, %v", n, err)
	}
	if rows, _ := v.db.ListFiles(""); len(rows) != 1 {
		t.Fatalf("files left = %d, want the other source's 1", len(rows))
	}
	if links, _ := v.db.ListFileLinks(""); len(links) != 0 {
		t.Fatal("links of a forgotten source remain")
	}
	if rules, _ := v.FileRules(nil, ""); len(rules) != 0 {
		t.Fatal("rules of a forgotten source remain")
	}
	if _, err := v.ForgetFileSource(a); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("forget twice = %v", err)
	}
}

func TestFiles_SurviveBackupAndRestore(t *testing.T) {
	v, sk := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("1", "/Leo/a.pdf", "a.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{})
	v.LinkFile(nil, f[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson, Role: "pagella"}, FileOriginManual)

	if err := v.SetBackupDir(filepath.Join(t.TempDir(), "bk")); err != nil {
		t.Fatal(err)
	}
	path, err := v.Backup()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CheckBackup(path, testPassword, sk); err != nil {
		t.Fatalf("a backup with links and no .enc files failed its check: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "restored")
	if _, err := RestoreBackup(path, dir, testPassword, sk); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	hits, _, err := r.SearchFiles(nil, FileQuery{Text: "a.pdf"})
	if err != nil || len(hits) != 1 || len(hits[0].Links) != 1 || hits[0].Links[0].Role != "pagella" {
		t.Fatalf("restored index = %+v, %v", hits, err)
	}
}

func TestFiles_SyncCannotTouchOtherSources(t *testing.T) {
	v, _ := tmpVault(t)
	a, b := fileSource(t, v), fileSource(t, v)
	v.UpsertFiles(a, []FileObservation{obs("1", "/a.pdf", "a.pdf")}, true)
	// A full listing of b says nothing about a's files.
	if res, _ := v.UpsertFiles(b, nil, true); res.Gone != 0 {
		t.Fatalf("a sync of one source tombstoned %d files of another", res.Gone)
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{}); total != 1 {
		t.Fatal("file of source a vanished")
	}
	if _, err := v.UpsertFiles("nonexistent", nil, true); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("unknown source = %v", err)
	}
}

func TestFiles_RejectsBadInput(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	var verr *ValidationError
	for _, o := range []FileObservation{
		{Key: "", Name: "a.pdf"},
		{Key: "k", Name: ""},
		{Key: "k", Name: "a\x00.pdf"},
		{Key: "k", Name: "a.pdf", Path: "/x/\x07y"},
	} {
		if _, err := v.UpsertFiles(src, []FileObservation{o}, false); !errors.As(err, &verr) {
			t.Errorf("UpsertFiles(%+v) = %v, want ValidationError", o, err)
		}
	}
	if _, err := v.AddFileSource("gopher", FileSourceConfig{Label: "x"}); !errors.As(err, &verr) {
		t.Errorf("unknown provider = %v", err)
	}
	if _, err := v.AddFileSource("local", FileSourceConfig{Label: "x"}); !errors.As(err, &verr) {
		t.Errorf("local source without a folder = %v", err)
	}
}

func TestDefaultFileTier(t *testing.T) {
	for name, want := range map[string]string{
		"contratto.pdf":            "sensitive",
		"Visura Camerale.pdf":      "sensitive",
		"SPID password.txt":        TierPersonal,
		"pin carta.pdf":            TierPersonal,
		"codici recupero CIE.pdf":  "sensitive", // an identity word alone is not a secret
		"scansione_passaporto.jpg": "sensitive",
	} {
		if got := DefaultFileTier(name, "/"+name); got != want {
			t.Errorf("DefaultFileTier(%q) = %q, want %q", name, got, want)
		}
	}
}
