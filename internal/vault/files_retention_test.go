package vault

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// linkedFile indexes a file at mtime and links it to holder with a role.
func linkedFile(t *testing.T, v *Vault, src, key, name string, mtime time.Time, holder, role string) string {
	t.Helper()
	o := obs(key, "/"+name, name)
	o.Mtime = ""
	if !mtime.IsZero() {
		o.Mtime = mtime.UTC().Format(time.RFC3339)
	}
	if _, err := v.UpsertFiles(src, []FileObservation{o}, false); err != nil {
		t.Fatal(err)
	}
	hits, _, _ := v.SearchFiles(nil, FileQuery{Text: name})
	if len(hits) != 1 {
		t.Fatalf("%q: %d hits", name, len(hits))
	}
	if holder != "" {
		if _, err := v.LinkFile(hits[0].ID, FileLinkInput{Holder: holder, Kind: FileKindClient, Role: role}, FileOriginManual); err != nil {
			t.Fatal(err)
		}
	}
	return hits[0].ID
}

func dueFiles(t *testing.T, v *Vault, allow FieldFilter, now time.Time) []ExpiryInfo {
	t.Helper()
	items, err := v.Expiring(90*24*time.Hour, allow, now)
	if err != nil {
		t.Fatal(err)
	}
	var out []ExpiryInfo
	for _, it := range items {
		if it.Kind == ExpiryFilesClosed {
			out = append(out, it)
		}
	}
	return out
}

func names(items []ExpiryInfo) string {
	var n []string
	for _, it := range items {
		n = append(n, it.File)
	}
	sort.Strings(n)
	return strings.Join(n, ",")
}

func TestRetention_LinkedClientFilesAfterTheirPraticheClose(t *testing.T) {
	now := time.Now().UTC()
	before, after := now.Add(-time.Hour), now.Add(time.Hour)
	v := tmpVaultKind(t, KindImpresa)
	for _, slug := range []string{"rossi", "verdi"} {
		mustSet(t, v, slug+":ruolo.tipo", "cliente")
	}
	mustSet(t, v, "bianchi:ruolo.tipo", "titolare")
	mustSet(t, v, "pratiche.cila-roma_persona", "rossi")
	mustSet(t, v, "pratiche.cila-roma_stato", "completata")
	mustSet(t, v, "pratiche.scia-verdi_persona", "verdi")
	mustSet(t, v, "pratiche.scia-verdi_stato", "in_attesa")
	mustSet(t, v, "pratiche.albo-bianchi_persona", "bianchi")
	mustSet(t, v, "pratiche.albo-bianchi_stato", "completata")
	src := fileSource(t, v)

	linkedFile(t, v, src, "1", "cie rossi.pdf", before, "rossi", "cie")
	linkedFile(t, v, src, "2", "permesso rossi.pdf", before, "rossi", "Permesso Soggiorno")
	linkedFile(t, v, src, "3", "contratto rossi.pdf", before, "rossi", "contratto")     // not an ID
	linkedFile(t, v, src, "4", "cie nuova rossi.pdf", after, "rossi", "cie")            // handed over for a later job
	linkedFile(t, v, src, "5", "cie senza data.pdf", time.Time{}, "rossi", "cie")       // can't tell when
	linkedFile(t, v, src, "6", "cie verdi.pdf", before, "verdi", "cie")                 // a pratica is still open
	linkedFile(t, v, src, "7", "cie bianchi.pdf", before, "bianchi", "cie")             // a titolare, not a client
	linkedFile(t, v, src, "8", "procura rossi.pdf", before, "rossi", "procura")         // no end date: in force
	gone := linkedFile(t, v, src, "9", "cie vecchia rossi.pdf", before, "rossi", "cie") // no longer at its source
	linkedFile(t, v, src, "10", "cie senza collegamento.pdf", before, "", "")           // linked to nobody
	if _, err := v.UpsertFiles(src, []FileObservation{{Key: "9", Deleted: true}}, false); err != nil {
		t.Fatal(err)
	}
	if hits, _, _ := v.SearchFiles(nil, FileQuery{Text: "vecchia", IncludeGone: true}); len(hits) != 1 || !hits[0].Gone || hits[0].ID != gone {
		t.Fatalf("file 9 should be gone: %+v", hits)
	}

	// Nothing until the user sets a period for scans.
	if got := dueFiles(t, v, nil, now); len(got) != 0 {
		t.Fatalf("no period set: %+v", got)
	}
	if err := v.SetScanDays(0); err != nil {
		t.Fatal(err)
	}
	got := dueFiles(t, v, nil, now)
	if names(got) != "cie rossi.pdf,permesso rossi.pdf" {
		t.Fatalf("due files = %q", names(got))
	}
	for _, it := range got {
		if it.Person != "rossi" || it.FieldID != "rossi:files.index" || it.FileID == "" || it.ExpiresOn != now.Format(dateLayout) || it.Kind != ExpiryFilesClosed {
			t.Fatalf("entry = %+v", it)
		}
	}
	if got[0].Document != "cie" && got[1].Document != "cie" {
		t.Fatalf("roles = %q %q", got[0].Document, got[1].Document)
	}
	if got[0].Document == got[1].Document {
		t.Fatalf("both files read as %q: the permesso's role should be permesso_soggiorno", got[0].Document)
	}

	// A period after the closing moves the day, and a short window leaves it out.
	if err := v.SetScanDays(30); err != nil {
		t.Fatal(err)
	}
	if items := dueFiles(t, v, nil, now); len(items) != 2 || items[0].DaysLeft != 30 {
		t.Fatalf("with 30 days: %+v", items)
	}
	short, _ := v.Expiring(10*24*time.Hour, nil, now)
	for _, it := range short {
		if it.Kind == ExpiryFilesClosed {
			t.Fatalf("a window of 10 days shows a file due in 30: %+v", it)
		}
	}
	v.SetScanDays(0)

	// What the caller can't see is left out, and never says a file may go on a partial view.
	for name, allow := range map[string]FieldFilter{
		"no files scope":          func(id, _ string) bool { return !strings.Contains(id, "files.index") },
		"leo's files only":        tokenFilter("leo:files.*,pratiche.*,*:ruolo.*"),
		"a pratica hidden":        func(id, _ string) bool { return !strings.HasPrefix(id, "pratiche.cila-roma_") },
		"ruolo hidden":            func(id, _ string) bool { return !strings.HasSuffix(id, ":ruolo.tipo") },
		"no persona of a pratica": func(id, _ string) bool { return id != "pratiche.cila-roma_persona" },
	} {
		if got := dueFiles(t, v, allow, now); len(got) != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got := dueFiles(t, v, tokenFilter("rossi:files.*,pratiche.*,*:ruolo.*"), now); len(got) != 2 {
		t.Errorf("a caller that sees rossi's files and their pratiche: %+v", got)
	}

	// A procura counts once its end date has passed, not before.
	mustSet(t, v, "rossi:procura.data_scadenza", now.AddDate(0, 0, 40).Format(dateLayout))
	if got := dueFiles(t, v, nil, now); names(got) != "cie rossi.pdf,permesso rossi.pdf" {
		t.Fatalf("procura still in force: %q", names(got))
	}
	mustSet(t, v, "rossi:procura.data_scadenza", now.AddDate(0, 0, -1).Format(dateLayout))
	if got := dueFiles(t, v, nil, now); names(got) != "cie rossi.pdf,permesso rossi.pdf,procura rossi.pdf" {
		t.Fatalf("procura ended: %q", names(got))
	}

	// The file is gone from its source: nothing left to delete.
	v.UpsertFiles(src, []FileObservation{{Key: "1", Deleted: true}}, false)
	if got := dueFiles(t, v, nil, now); strings.Contains(names(got), "cie rossi.pdf") {
		t.Fatalf("a file already gone is still listed: %q", names(got))
	}

	// A reopened pratica takes them all back.
	mustSet(t, v, "pratiche.cila-roma_stato", "in_attesa")
	if got := dueFiles(t, v, nil, now); len(got) != 0 {
		t.Fatalf("reopened: %+v", got)
	}

	// A personal vault keeps family files: no such entries.
	p := tmpVaultKind(t, KindPersona)
	p.SetScanDays(0)
	mustSet(t, p, "leo:famiglia.relazione", "figlio")
	mustSet(t, p, "pratiche.cie-leo_persona", "leo")
	mustSet(t, p, "pratiche.cie-leo_stato", "completata")
	psrc := fileSource(t, p)
	linkedFile(t, p, psrc, "1", "cie leo.pdf", before, "leo", "cie")
	if got := dueFiles(t, p, nil, now); len(got) != 0 {
		t.Fatalf("personal vault: %+v", got)
	}
}
