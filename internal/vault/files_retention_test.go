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

func TestRetention_AFileWithAnotherLinkIsKept(t *testing.T) {
	now := time.Now().UTC()
	before := now.Add(-time.Hour)
	v := tmpVaultKind(t, KindImpresa)
	for _, slug := range []string{"rossi", "bianchi", "verdi"} {
		mustSet(t, v, slug+":ruolo.tipo", "cliente")
	}
	mustSet(t, v, "pratiche.cila-rossi_persona", "rossi")
	mustSet(t, v, "pratiche.cila-rossi_stato", "completata")
	mustSet(t, v, "pratiche.scia-bianchi_persona", "bianchi")
	mustSet(t, v, "pratiche.scia-bianchi_stato", "in_attesa") // open
	mustSet(t, v, "pratiche.albo-verdi_persona", "verdi")
	mustSet(t, v, "pratiche.albo-verdi_stato", "completata")
	mustSet(t, v, "pratiche.tari-2026_stato", "in_preparazione")
	if err := v.SetScanDays(0); err != nil {
		t.Fatal(err)
	}
	src := fileSource(t, v)

	link := func(id, holder, role string) {
		in := FileLinkInput{Holder: holder, Kind: FileKindClient, Role: role}
		if _, err := v.LinkFile(id, in, FileOriginManual); err != nil {
			t.Fatal(err)
		}
	}
	pair := linkedFile(t, v, src, "1", "coniugi.pdf", before, "rossi", "cie") // rossi done, bianchi open
	link(pair, "bianchi", "cie")
	pratica := linkedFile(t, v, src, "2", "cie rossi per tari.pdf", before, "rossi", "cie")
	if _, err := v.LinkFile(pratica, FileLinkInput{Kind: FileKindPratica, Pratica: "tari-2026"}, FileOriginManual); err != nil {
		t.Fatal(err)
	}
	both := linkedFile(t, v, src, "3", "cie rossi e verdi.pdf", before, "rossi", "cie") // two done clients, both as an ID
	link(both, "verdi", "cie")
	contract := linkedFile(t, v, src, "5", "rossi e verdi insieme.pdf", before, "rossi", "cie") // verdi needs it as a contract
	link(contract, "verdi", "contratto")
	linkedFile(t, v, src, "4", "cie solo rossi.pdf", before, "rossi", "cie")

	got := dueFiles(t, v, nil, now)
	if names(got) != "cie rossi e verdi.pdf,cie solo rossi.pdf" {
		t.Fatalf("due = %q (a file also linked to an open client or pratica, or needed as something else, must stay)", names(got))
	}
	// Reported once, whichever client's link carries the ID role.
	count := 0
	for _, it := range got {
		if it.File == "cie rossi e verdi.pdf" {
			count++
			if it.Person != "rossi" || it.Document != "cie" {
				t.Fatalf("entry = %+v", it)
			}
		}
	}
	if count != 1 {
		t.Fatalf("a file shared by two done clients was reported %d times", count)
	}
	// What the caller can't see of a file's links keeps it.
	noVerdi := func(id, tier string) bool { return !strings.HasPrefix(id, "verdi:") }
	if got := dueFiles(t, v, noVerdi, now); strings.Contains(names(got), "rossi e verdi") {
		t.Fatalf("a caller that can't see one of a file's links was told it may go: %q", names(got))
	}
}

func TestRetention_AnUndatedProcuraKeepsTheOthers(t *testing.T) {
	now := time.Now().UTC()
	before := now.Add(-time.Hour)
	v := tmpVaultKind(t, KindImpresa)
	mustSet(t, v, "rossi:ruolo.tipo", "cliente")
	mustSet(t, v, "pratiche.cila-rossi_persona", "rossi")
	mustSet(t, v, "pratiche.cila-rossi_stato", "completata")
	if err := v.SetScanDays(0); err != nil {
		t.Fatal(err)
	}
	src := fileSource(t, v)
	linkedFile(t, v, src, "1", "procura rossi.pdf", before, "rossi", "procura")

	mustSet(t, v, "rossi:procura.data_scadenza", now.AddDate(0, 0, -5).Format(dateLayout))
	if got := dueFiles(t, v, nil, now); len(got) != 1 {
		t.Fatalf("one procura, ended: %+v", got)
	}
	// A second copy with no end date is still in force.
	mustSet(t, v, "rossi:procura2.ente", "SUE Comune di Napoli")
	if got := dueFiles(t, v, nil, now); len(got) != 0 {
		t.Fatalf("an undated second procura: %+v", got)
	}
	mustSet(t, v, "rossi:procura2.data_scadenza", now.AddDate(0, 0, -2).Format(dateLayout))
	if got := dueFiles(t, v, nil, now); len(got) != 1 {
		t.Fatalf("both ended: %+v", got)
	}
}

// A file linked to two clients is only reported once it was untouched since the
// EARLIER of their closings: one modified after the first client's pratiche
// closed may have been handed over for a new job.
func TestRetention_ASharedFileUsesTheEarlierClosing(t *testing.T) {
	now := time.Now().UTC()
	v := tmpVaultKind(t, KindImpresa)
	for _, slug := range []string{"rossi", "verdi"} {
		mustSet(t, v, slug+":ruolo.tipo", "cliente")
		mustSet(t, v, "pratiche.p-"+slug+"_persona", slug)
		mustSet(t, v, "pratiche.p-"+slug+"_stato", "completata")
	}
	setClosedAt(t, v, "pratiche.p-rossi_stato", now.AddDate(0, 0, -90))
	setClosedAt(t, v, "pratiche.p-verdi_stato", now.AddDate(0, 0, -1))
	if err := v.SetScanDays(0); err != nil {
		t.Fatal(err)
	}
	src := fileSource(t, v)
	for key, age := range map[string]int{"1": 30, "2": 100} {
		id := linkedFile(t, v, src, key, "cie-"+key+".pdf", now.AddDate(0, 0, -age), "rossi", "cie")
		if _, err := v.LinkFile(id, FileLinkInput{Holder: "verdi", Kind: FileKindClient, Role: "cie"}, FileOriginManual); err != nil {
			t.Fatal(err)
		}
	}
	got := dueFiles(t, v, nil, now)
	if names(got) != "cie-2.pdf" {
		t.Fatalf("due = %q: a file modified 30 days ago, after rossi's pratiche closed 90 days ago, must stay", names(got))
	}
	// Due on the later client's closing, not the earlier one's.
	if len(got) == 1 && got[0].ExpiresOn != now.AddDate(0, 0, -1).Format(dateLayout) {
		t.Fatalf("expires on %s, want the later closing", got[0].ExpiresOn)
	}
}
