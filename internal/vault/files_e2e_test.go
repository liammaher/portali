package vault

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Everything the file index keeps in vault.db comes back from a backup: a
// folder source, an API source's settings (never its tokens), a source fed by
// an agent, links, rules, pending suggestions and pending proposals.
func TestFiles_EverythingSurvivesBackupAndRestore(t *testing.T) {
	v, sk := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "pratiche.tari-2026_stato", "in_preparazione")

	folder := fileSource(t, v)
	api, err := v.AddFileSource("onedrive", FileSourceConfig{Label: "OneDrive", Account: "liam@example.it", ClientID: "client-1", Tenant: "common", DriveID: "b!mine"})
	if err != nil {
		t.Fatal(err)
	}
	v.UpsertFiles(folder, []FileObservation{obs("1", "/Leo/a.pdf", "a.pdf"), obs("2", "/b.pdf", "b.pdf")}, true)
	v.UpsertFiles(api.ID, []FileObservation{obs("item-1", "/Documenti/c.pdf", "c.pdf")}, true)
	files, _, _ := v.SearchFiles(nil, FileQuery{Limit: 100})
	byName := map[string]string{}
	for _, f := range files {
		byName[f.Name] = f.ID
	}
	v.LinkFile(nil, byName["a.pdf"], FileLinkInput{Holder: "leo", Kind: FileKindPerson, Role: "pagella"}, FileOriginManual)
	v.LinkFile(nil, byName["b.pdf"], FileLinkInput{Kind: FileKindPratica, Pratica: "tari-2026"}, FileOriginManual)
	v.AddFileRule(nil, folder, FileRuleInput{Path: "/Leo", Holder: "leo", Kind: FileKindPerson})
	v.SuggestFileLink(tokenFilter("*:files.*"), byName["c.pdf"], FileLinkInput{Holder: "leo", Kind: FileKindPerson}, "agent", "its folder")
	accepted, _ := v.ProposeFile(tokenFilter("files.*"), driveProposal("d1", "d.pdf"), "claude")
	v.ResolveFileProposal(nil, accepted.ID, true)
	if _, err := v.ProposeFile(tokenFilter("files.*"), driveProposal("e1", "e.pdf"), "claude"); err != nil { // stays pending
		t.Fatal(err)
	}

	if err := v.SetBackupDir(filepath.Join(t.TempDir(), "bk")); err != nil {
		t.Fatal(err)
	}
	path, err := v.Backup()
	if err != nil {
		t.Fatal(err)
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

	srcs, _ := r.FileSources(nil)
	kinds := map[string]FileSourceInfo{}
	for _, s := range srcs {
		kinds[s.Label] = s
	}
	if len(srcs) != 3 || kinds["OneDrive"].ClientID != "client-1" || kinds["OneDrive"].DriveID != "b!mine" || kinds["OneDrive"].Files != 1 ||
		kinds["Google Drive (found through Claude)"].Via != ViaConnector || kinds["Google Drive (found through Claude)"].Files != 1 {
		t.Fatalf("sources = %+v", srcs)
	}
	all, total, err := r.SearchFiles(nil, FileQuery{Limit: 100})
	if err != nil || total != 4 {
		t.Fatalf("files = %d, %v", total, err)
	}
	links := map[string]string{}
	for _, f := range all {
		links[f.Name] = linkSummaryForTest(f)
	}
	if links["a.pdf"] != "leo:pagella" || links["b.pdf"] != "pratica:tari-2026" {
		t.Fatalf("links = %v", links)
	}
	if rules, _ := r.FileRules(nil, ""); len(rules) != 1 || rules[0].Holder != "leo" {
		t.Fatalf("rules = %+v", rules)
	}
	if sugs, _ := r.FileSuggestions(nil); len(sugs) != 1 || sugs[0].Reason != "its folder" {
		t.Fatalf("suggestions = %+v", sugs)
	}
	if props, _ := r.FileProposals(nil); len(props) != 1 || props[0].Name != "e.pdf" {
		t.Fatalf("proposals = %+v", props)
	}
	// The restored vault carries on: a pending proposal can be accepted.
	props, _ := r.FileProposals(nil)
	if f, err := r.ResolveFileProposal(nil, props[0].ID, true); err != nil || f.Name != "e.pdf" {
		t.Fatalf("accepting after a restore: %+v, %v", f, err)
	}
}

func linkSummaryForTest(f FileInfo) string {
	out := ""
	for _, l := range f.Links {
		switch {
		case l.Kind == FileKindPratica:
			out += "pratica:" + l.PraticaSlug
		default:
			out += l.Holder + ":" + l.Role
		}
	}
	return out
}

// Syncs, searches, links, suggestions, proposals and person removal all at
// once: no deadlock (the lock order), no race (-race), and the index stays
// consistent.
func TestFiles_ConcurrentUseDoesNotDeadlock(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	a, b := fileSource(t, v), fileSource(t, v)

	var batch []FileObservation
	for i := 0; i < 150; i++ {
		batch = append(batch, obs(fmt.Sprint("k", i), fmt.Sprintf("/Leo/f%d.pdf", i), fmt.Sprintf("f%d.pdf", i)))
	}
	v.AddFileRule(nil, a, FileRuleInput{Path: "/Leo", Holder: "leo", Kind: FileKindPerson})

	done := make(chan struct{})
	var wg sync.WaitGroup
	run := func(f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-done:
					return
				default:
					f(i)
				}
			}
		}()
	}
	agent := tokenFilter("*:files.*")
	run(func(i int) { // a long sync of source a
		id := fmt.Sprintf("sync-aaaa-%d", i)
		v.UpsertFileBatch(a, id, true, batch[:75])
		v.UpsertFileBatch(a, id, false, batch[75:])
		v.FinishFileSync(nil, a, id, true)
	})
	run(func(i int) { v.UpsertFiles(b, batch[:40], i%5 == 0) })
	run(func(i int) { v.SearchFiles(agent, FileQuery{Text: "f1"}); v.FileSources(agent) })
	run(func(i int) {
		hits, _, _ := v.SearchFiles(nil, FileQuery{Text: "f2", Limit: 5})
		for _, h := range hits {
			v.LinkFile(nil, h.ID, FileLinkInput{Kind: FileKindPerson, Role: "x"}, FileOriginManual)
			v.SuggestFileLink(agent, h.ID, FileLinkInput{Holder: "leo", Kind: FileKindClient}, "agent", "")
		}
		sugs, _ := v.FileSuggestions(nil)
		for _, s := range sugs {
			v.ResolveFileSuggestion(nil, s.ID, i%2 == 0)
		}
	})
	run(func(i int) {
		if p, err := v.ProposeFile(tokenFilter("files.*"), driveProposal(fmt.Sprint("p", i%30), fmt.Sprintf("p%d.pdf", i%30)), "claude"); err == nil {
			v.ResolveFileProposal(nil, p.ID, i%3 != 0)
		}
	})
	run(func(i int) { v.PruneFiles(); v.Expiring(24*time.Hour, nil, time.Now()) })

	time.Sleep(1500 * time.Millisecond)
	// A person's removal, a backup and a lock in the middle of it.
	removed := make(chan error, 1)
	go func() { _, err := v.RemovePerson("leo"); removed <- err }()
	select {
	case err := <-removed:
		if err != nil {
			t.Logf("RemovePerson: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("RemovePerson hung: a deadlock between the file index and the person lock")
	}
	close(done)
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("a file operation hung")
	}

	// Consistent afterwards: every link points at a file that exists, nothing for leo remains.
	files, _, err := v.SearchFiles(nil, FileQuery{Limit: 1000, IncludeGone: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		for _, l := range f.Links {
			if l.Holder == "leo" {
				t.Fatalf("a link for the removed leo survived on %s", f.Name)
			}
		}
	}
	if links, _ := v.db.ListFileLinks(""); len(links) > 0 {
		ids := map[string]bool{}
		all, _ := v.db.ListFiles("")
		for _, f := range all {
			ids[f.ID] = true
		}
		for _, l := range links {
			if !ids[l.FileID] {
				t.Fatalf("a link points at a file that is gone from the index: %+v", l)
			}
		}
	}
}
