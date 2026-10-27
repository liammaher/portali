package vault

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFilesBug_UnlinkedRuleLinkStaysUnlinkedAfterAnUpdate(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.AddFileRule(src, FileRuleInput{Path: "/Leo", Holder: "leo", Kind: FileKindPerson})
	v.UpsertFiles(src, []FileObservation{obs("1", "/Leo/a.pdf", "a.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{})
	if len(f[0].Links) != 1 {
		t.Fatal("rule didn't link")
	}
	v.UnlinkFile(nil, f[0].Links[0].ID)

	// The file changes (new mtime) in the same place.
	o := obs("1", "/Leo/a.pdf", "a.pdf")
	o.Mtime = "2026-11-01T10:00:00Z"
	v.UpsertFiles(src, []FileObservation{o}, true)
	f, _, _ = v.SearchFiles(nil, FileQuery{})
	if len(f[0].Links) != 0 {
		t.Fatal("a link the user removed came back when the file changed")
	}
}

func TestFilesBug_RuleNeedsAPath(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	var verr *ValidationError
	if _, _, err := v.AddFileRule(src, FileRuleInput{Path: "", Kind: FileKindPerson}); !errors.As(err, &verr) {
		t.Fatalf("a rule with no path would link every file: %v", err)
	}
}

func TestFilesBug_SearchSortsBeforeItCuts(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	var batch []FileObservation
	for i := 0; i < 30; i++ {
		batch = append(batch, obs(fmt.Sprint(i), fmt.Sprintf("/%02d.pdf", i), fmt.Sprintf("file-%02d.pdf", 29-i)))
	}
	v.UpsertFiles(src, batch, true)
	hits, total, _ := v.SearchFiles(nil, FileQuery{Limit: 3})
	if total != 30 || len(hits) != 3 || hits[0].Name != "file-00.pdf" || hits[2].Name != "file-02.pdf" {
		var names []string
		for _, h := range hits {
			names = append(names, h.Name)
		}
		t.Fatalf("first 3 by name = %v (total %d)", names, total)
	}
}

func TestFilesBug_OddNamesCannotFailAWholeBatch(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	for _, name := range []string{"   ", "a‮gpj.exe", "..."} {
		o := obs("k", "/x", name)
		if err := o.Check(); err == nil {
			t.Errorf("name %q passes Check but the vault refuses it", name)
		}
	}
	_ = src
}

func TestFilesBug_SuggestionsAreBounded(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	var batch []FileObservation
	for i := 0; i < maxPendingSuggestions+10; i++ {
		batch = append(batch, obs(fmt.Sprint(i), fmt.Sprintf("/%d.pdf", i), fmt.Sprintf("%d.pdf", i)))
	}
	v.UpsertFiles(src, batch, true)
	files, _, _ := v.SearchFiles(nil, FileQuery{Limit: 1000})
	agent := tokenFilter("*:files.*")

	// The same suggestion again is the same row.
	for i := 0; i < 5; i++ {
		if _, err := v.SuggestFileLink(agent, files[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, "agent", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if sugs, _ := v.FileSuggestions(nil); len(sugs) != 1 {
		t.Fatalf("the same suggestion five times left %d rows", len(sugs))
	}
	// Different files fill the queue up to the cap, then are refused.
	var refused int
	for i, f := range files {
		if _, err := v.SuggestFileLink(agent, f.ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, fmt.Sprint("agent-", i%6), ""); err != nil {
			refused++
		}
	}
	sugs, _ := v.FileSuggestions(nil)
	if len(sugs) != maxPendingSuggestions || refused != 10 {
		t.Fatalf("%d pending (cap %d), %d refused (want 10)", len(sugs), maxPendingSuggestions, refused)
	}
	// Deciding on one makes room.
	v.ResolveFileSuggestion(nil, sugs[0].ID, false)
	if _, err := v.SuggestFileLink(agent, files[len(files)-1].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, "agent-fresh", ""); err != nil {
		t.Fatalf("after room was made: %v", err)
	}
}

func TestFilesBug_PruneDuringASyncDoesNotBreakTheNextBatch(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("x", "/x.pdf", "x.pdf"), obs("y", "/y.pdf", "y.pdf")}, true)
	v.UpsertFiles(src, []FileObservation{obs("y", "/y.pdf", "y.pdf")}, true) // x is gone

	if _, err := v.UpsertFileBatch(src, "sync-aaaa-1", true, []FileObservation{obs("y", "/y.pdf", "y.pdf")}); err != nil {
		t.Fatal(err)
	}
	if n, _ := v.PruneFiles(); n != 1 {
		t.Fatalf("prune = %d", n)
	}
	// The prune ended that sync's memory. Its next batch is refused rather
	// than starting a memory that has forgotten y: finishing that would mark y gone.
	if _, err := v.UpsertFileBatch(src, "sync-aaaa-1", false, []FileObservation{obs("x", "/x.pdf", "x.pdf")}); !errors.Is(err, ErrSyncExpired) {
		t.Fatalf("batch after a prune = %v, want ErrSyncExpired", err)
	}
	if _, err := v.FinishFileSync(nil, src, "sync-aaaa-1", false); !errors.Is(err, ErrSyncExpired) {
		t.Fatalf("finish after a prune = %v, want ErrSyncExpired", err)
	}
	// A new sync works, with a cache that doesn't hold the pruned row.
	if _, err := v.UpsertFileBatch(src, "sync-aaaa-2", true, []FileObservation{obs("x", "/x.pdf", "x.pdf"), obs("y", "/y.pdf", "y.pdf")}); err != nil {
		t.Fatalf("new sync after a prune: %v", err)
	}
	if res, err := v.FinishFileSync(nil, src, "sync-aaaa-2", false); err != nil || res.Gone != 0 {
		t.Fatalf("finish = %+v, %v", res, err)
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{}); total != 2 {
		t.Fatalf("%d files listed, want 2", total)
	}
}

func TestFilesBug_LockEndsARunningSync(t *testing.T) {
	v, sk := tmpVault(t)
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("a", "/a.pdf", "a.pdf"), obs("b", "/b.pdf", "b.pdf")}, true)
	if _, err := v.UpsertFileBatch(src, "sync-bbbb-1", true, []FileObservation{obs("a", "/a.pdf", "a.pdf")}); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if v.fileSyncs != nil && len(v.fileSyncs) != 0 {
		t.Fatal("a locked vault still remembers paths of a running sync")
	}
	if _, err := v.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	// Whatever the vault remembers, a sync begun before the lock can't finish:
	// only batch 1 of 2 had arrived, and finishing would mark b gone.
	if _, err := v.UpsertFileBatch(src, "sync-bbbb-1", false, nil); !errors.Is(err, ErrSyncExpired) {
		t.Fatalf("batch after a lock = %v, want ErrSyncExpired", err)
	}
	if _, err := v.FinishFileSync(nil, src, "sync-bbbb-1", false); !errors.Is(err, ErrSyncExpired) {
		t.Fatalf("finish after a lock = %v, want ErrSyncExpired", err)
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{}); total != 2 {
		t.Fatalf("%d files listed, want 2", total)
	}
}

func TestFilesBug_OnlyAMoveIntoARuledFolderLinks(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.AddFileRule(src, FileRuleInput{Path: "/Leo", Holder: "leo", Kind: FileKindPerson})
	// An API source's key is an item ID that survives a move.
	v.UpsertFiles(src, []FileObservation{obs("item-1", "/Altro/a.pdf", "a.pdf")}, true)
	if hits, _, _ := v.SearchFiles(nil, FileQuery{}); len(hits[0].Links) != 0 {
		t.Fatal("linked outside the ruled folder")
	}
	res, _ := v.UpsertFiles(src, []FileObservation{obs("item-1", "/Leo/a.pdf", "a.pdf")}, true)
	if res.Updated != 1 || res.Linked != 1 {
		t.Fatalf("move into the folder = %+v", res)
	}
	hits, _, _ := v.SearchFiles(nil, FileQuery{})
	if len(hits) != 1 || len(hits[0].Links) != 1 {
		t.Fatalf("after the move: %+v", hits)
	}
}

func TestFilesBug_LargeSyncIsNotQuadratic(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	const n, batch = 6000, 200
	start := time.Now()
	for i := 0; i < n; i += batch {
		var b []FileObservation
		for j := i; j < i+batch; j++ {
			b = append(b, obs(fmt.Sprint("k", j), fmt.Sprintf("/d%d/f%d.pdf", j%50, j), fmt.Sprintf("f%d.pdf", j)))
		}
		if _, err := v.UpsertFileBatch(src, "sync-large-1", i == 0, b); err != nil {
			t.Fatal(err)
		}
	}
	first := time.Since(start)
	start = time.Now()
	for i := 0; i < n; i += batch {
		var b []FileObservation
		for j := i; j < i+batch; j++ {
			b = append(b, obs(fmt.Sprint("k", j), fmt.Sprintf("/d%d/f%d.pdf", j%50, j), fmt.Sprintf("f%d.pdf", j)))
		}
		if _, err := v.UpsertFileBatch(src, "sync-large-2", i == 0, b); err != nil {
			t.Fatal(err)
		}
	}
	second := time.Since(start)
	t.Logf("%d files: first sync %v, unchanged resync %v", n, first, second)
	if second > 20*time.Second || first > 60*time.Second {
		t.Fatalf("too slow: %v / %v", first, second)
	}

	// The cost of a batch must not grow with the size of the index: a batch
	// into an index of 6000 takes about as long as one into an empty one.
	tm := func(syncID string, begin bool, from int) time.Duration {
		var b []FileObservation
		for j := from; j < from+batch; j++ {
			b = append(b, obs(fmt.Sprint("k", j), fmt.Sprintf("/d%d/f%d.pdf", j%50, j), fmt.Sprintf("f%d.pdf", j)))
		}
		start := time.Now()
		if _, err := v.UpsertFileBatch(src, syncID, begin, b); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	}
	early := tm("sync-large-3", true, 0)  // builds the cache once
	mid := tm("sync-large-3", false, 200) // cached
	late := tm("sync-large-3", false, 5800)
	if late > 5*mid+100*time.Millisecond {
		t.Fatalf("batch time grows with the index: %v (cached, early) vs %v (late); first batch %v", mid, late, early)
	}
}

// A restart of the server (or anything else that makes the vault forget a
// running sync) in the middle of one must not turn the batches after it into
// a "whole" sync that tombstones the files only earlier batches named.
func TestFilesBug_ALostSyncCannotBeResumedAndFinished(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("a", "/a.pdf", "a.pdf"), obs("b", "/b.pdf", "b.pdf")}, true)

	// Batch 1 (a) is lost with the memory; batch 2 (b) arrives with no begin.
	if _, err := v.UpsertFileBatch(src, "sync-cccc-1", false, []FileObservation{obs("b", "/b.pdf", "b.pdf")}); !errors.Is(err, ErrSyncExpired) {
		t.Fatalf("batch of an unknown sync = %v, want ErrSyncExpired", err)
	}
	if _, err := v.FinishFileSync(nil, src, "sync-cccc-1", false); !errors.Is(err, ErrSyncExpired) {
		t.Fatalf("finish of an unknown sync = %v", err)
	}

	// Two clients syncing one source: the second begin ends the first, whose
	// later batches and finish are refused (they'd tombstone the other's files).
	v.UpsertFileBatch(src, "sync-dddd-1", true, []FileObservation{obs("a", "/a.pdf", "a.pdf")})
	v.UpsertFileBatch(src, "sync-dddd-2", true, []FileObservation{obs("b", "/b.pdf", "b.pdf")})
	if _, err := v.UpsertFileBatch(src, "sync-dddd-1", false, nil); !errors.Is(err, ErrSyncExpired) {
		t.Fatalf("batch of a replaced sync = %v", err)
	}
	if _, err := v.FinishFileSync(nil, src, "sync-dddd-1", false); !errors.Is(err, ErrSyncExpired) {
		t.Fatalf("finish of a replaced sync = %v", err)
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{}); total != 2 {
		t.Fatalf("%d files listed, want 2", total)
	}
}

// --- found by the review ---------------------------------------------------

func TestFilesReview_CredentialWordOnALongPathStillMakesItPersonal(t *testing.T) {
	long := "/" + strings.Repeat("cartella/", 60) // well past 400 bytes
	for _, p := range []string{long + "Password/note.txt", long + "SPID/recovery pin.txt"} {
		if got := DefaultFileTier("note.txt", p); got != TierPersonal {
			t.Errorf("DefaultFileTier on a %d-byte path ending %q = %q, want personal", len(p), p[len(p)-20:], got)
		}
	}
	if got := DefaultFileTier("note.txt", long+"Archivio/note.txt"); got != "sensitive" {
		t.Errorf("a long harmless path = %q", got)
	}
}

func TestFilesReview_AFileWhosePersonLinkIsGoneStaysOutOfTheOwnersScope(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("1", "/Leo/passaporto leo.pdf", "passaporto leo.pdf"), obs("2", "/Casa/contratto.pdf", "contratto.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{Text: "passaporto"})
	link, _ := v.LinkFile(f[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, FileOriginManual)

	owner, everyone := tokenFilter("files.*"), tokenFilter("*:files.*")
	seen := func(allow FieldFilter) int { _, n, _ := v.SearchFiles(allow, FileQuery{Text: "passaporto"}); return n }
	if seen(owner) != 0 {
		t.Fatal("owner scope sees leo's file while it is linked")
	}
	if err := v.UnlinkFile(nil, link.ID); err != nil {
		t.Fatal(err)
	}
	if seen(owner) != 0 {
		t.Fatal("unlinking leo's last link handed his file to the owner's scope")
	}
	if seen(everyone) != 1 || seen(nil) != 1 {
		t.Fatal("a caller reaching every person, and the user, still see it")
	}
	if _, total, _ := v.SearchFiles(owner, FileQuery{Text: "contratto"}); total != 1 {
		t.Fatal("a file that never had a person link is still the owner's")
	}

	// Removing the person does the same.
	v.LinkFile(f[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, FileOriginManual)
	if _, err := v.RemovePerson("leo"); err != nil {
		t.Fatal(err)
	}
	if seen(owner) != 0 {
		t.Fatal("removing leo handed his file to the owner's scope")
	}
}

func TestFilesReview_ASyncTokenLeavesFilesItCannotSeeAlone(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	var seed []FileObservation
	for i := 0; i < 6; i++ {
		seed = append(seed, obs(fmt.Sprint("o", i), fmt.Sprintf("/Casa/%d.pdf", i), fmt.Sprintf("casa-%d.pdf", i)))
	}
	seed = append(seed, obs("leo", "/Leo/scuola.pdf", "scuola.pdf"), obs("pw", "/pw.txt", "SPID password.txt"))
	v.UpsertFiles(src, seed, true)
	scuola, _, _ := v.SearchFiles(nil, FileQuery{Text: "scuola"})
	v.LinkFile(scuola[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, FileOriginManual)

	token := tokenFilter("files.*")
	// It resends the hidden files' keys with other names, and a full listing
	// that leaves them out.
	res, err := v.UpsertFilesIf(token, src, []FileObservation{
		obs("leo", "/Leo/scuola.pdf", "hacked.pdf"), obs("pw", "/pw.txt", "other.txt"),
		obs("o0", "/Casa/0.pdf", "casa-0.pdf"), obs("o1", "/Casa/1.pdf", "casa-1.pdf"), obs("o2", "/Casa/2.pdf", "casa-2.pdf"),
		obs("o3", "/Casa/3.pdf", "casa-3.pdf"), obs("o4", "/Casa/4.pdf", "casa-4.pdf"), obs("o5", "/Casa/5.pdf", "casa-5.pdf"),
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated != 0 || res.Gone != 0 || res.Added != 0 || res.Unchanged != 6 {
		t.Fatalf("a token's sync counted hidden files: %+v", res)
	}
	for _, name := range []string{"scuola.pdf", "SPID password.txt"} {
		if _, n, _ := v.SearchFiles(nil, FileQuery{Text: name}); n != 1 {
			t.Fatalf("%q was changed by a token that can't see it", name)
		}
	}

	// A full listing that omits what the token can see is the user's call.
	if _, err := v.UpsertFilesIf(token, src, nil, true); err == nil {
		t.Fatal("a token emptied the source with an empty full listing")
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{}); total != 8 {
		t.Fatalf("%d files after the refused wipe, want 8", total)
	}
	// Nor does a finish that names nothing.
	v.UpsertFileBatchIf(token, src, "sync-eeee-1", true, nil)
	if _, err := v.FinishFileSync(token, src, "sync-eeee-1", false); err == nil {
		t.Fatal("finish of a sync that named nothing marked the source gone")
	}
	// The visible count a token gets excludes what it can't see.
	srcs, _ := v.FileSources(token)
	if srcs[0].Files != 6 {
		t.Fatalf("token sees %d files in the source, want 6", srcs[0].Files)
	}
	if all, _ := v.FileSources(nil); all[0].Files != 8 {
		t.Fatalf("user sees %d files, want 8", all[0].Files)
	}
}

func TestFilesReview_MostOfASourceGoingMissingNeedsConfirming(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	var all []FileObservation
	for i := 0; i < 30; i++ {
		all = append(all, obs(fmt.Sprint(i), fmt.Sprintf("/%d.pdf", i), fmt.Sprintf("%d.pdf", i)))
	}
	v.UpsertFiles(src, all, true)

	// A partial listing: 5 of 30 present.
	if _, err := v.UpsertFileBatch(src, "sync-ffff-1", true, all[:5]); err != nil {
		t.Fatal(err)
	}
	if _, err := v.FinishFileSync(nil, src, "sync-ffff-1", false); err == nil {
		t.Fatal("25 of 30 files marked gone without confirmation")
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{Limit: 1000}); total != 30 {
		t.Fatalf("a refused finish changed the index: %d files", total)
	}
	// The sync is still running; the user confirms.
	res, err := v.FinishFileSync(nil, src, "sync-ffff-1", true)
	if err != nil || res.Gone != 25 {
		t.Fatalf("confirmed finish = %+v, %v", res, err)
	}
	// A few files going is routine.
	v.UpsertFiles(src, all, true)
	if _, err := v.UpsertFiles(src, all[:25], true); err != nil {
		t.Fatalf("5 of 30 gone: %v", err)
	}
}

func TestFilesReview_AWriteOutsideASyncEndsIt(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("k1", "/a.pdf", "a.pdf")}, true)
	if _, err := v.UpsertFileBatch(src, "sync-gggg-1", true, []FileObservation{obs("k1", "/a.pdf", "a.pdf")}); err != nil {
		t.Fatal(err)
	}
	// Someone else adds k2 and renames k1 without a sync ID.
	v.UpsertFiles(src, []FileObservation{obs("k1", "/a-new.pdf", "a-new.pdf"), obs("k2", "/b.pdf", "b.pdf")}, false)
	if _, err := v.UpsertFileBatch(src, "sync-gggg-1", false, []FileObservation{obs("k2", "/b.pdf", "b.pdf")}); !errors.Is(err, ErrSyncExpired) {
		t.Fatalf("batch after an outside write = %v, want ErrSyncExpired (its cache is stale)", err)
	}
	hits, total, _ := v.SearchFiles(nil, FileQuery{})
	if total != 2 || hits[0].Name != "a-new.pdf" || hits[1].Name != "b.pdf" {
		t.Fatalf("index = %+v", hits)
	}
}

func TestFilesReview_PraticaProbeAndHolderBoundaries(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "pratiche.tari-2026_stato", "in_preparazione")
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("1", "/a.pdf", "a.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{})

	blind := tokenFilter("files.*")
	_, errKnown := v.SuggestFileLink(blind, f[0].ID, FileLinkInput{Kind: FileKindPratica, Pratica: "tari-2026"}, "agent", "")
	_, errUnknown := v.SuggestFileLink(blind, f[0].ID, FileLinkInput{Kind: FileKindPratica, Pratica: "nessuna-pratica"}, "agent", "")
	if errKnown == nil || errUnknown == nil || strings.ReplaceAll(errKnown.Error(), "tari-2026", "X") != strings.ReplaceAll(errUnknown.Error(), "nessuna-pratica", "X") {
		t.Fatalf("a token with no pratiche scope can tell a real pratica from a missing one: %v / %v", errKnown, errUnknown)
	}
	if _, err := v.SuggestFileLink(tokenFilter("files.*,pratiche.*"), f[0].ID, FileLinkInput{Kind: FileKindPratica, Pratica: "tari-2026"}, "agent", ""); err != nil {
		t.Fatalf("with the scope: %v", err)
	}

	// A grant for leo's files can't unlink, accept or remove another holder's.
	owned, _ := v.LinkFile(f[0].ID, FileLinkInput{Kind: FileKindPerson}, FileOriginManual)
	leoGrant := tokenFilter("leo:files.*")
	if err := v.UnlinkFile(leoGrant, owned.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("leo's grant unlinked the owner's link: %v", err)
	}
	sug, _ := v.SuggestFileLink(nil, f[0].ID, FileLinkInput{Kind: FileKindPerson}, "agent", "")
	if _, err := v.ResolveFileSuggestion(leoGrant, sug.ID, true); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("leo's grant accepted a suggestion for the owner: %v", err)
	}
	rule, _, _ := v.AddFileRule(src, FileRuleInput{Path: "/x", Kind: FileKindPerson})
	if err := v.RemoveFileRule(leoGrant, rule.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("leo's grant removed the owner's rule: %v", err)
	}
	if rules, _ := v.FileRules(leoGrant, ""); len(rules) != 0 {
		t.Fatal("leo's grant lists the owner's rules")
	}
	if sugs, _ := v.FileSuggestions(leoGrant); len(sugs) != 0 {
		t.Fatal("leo's grant lists the owner's suggestions")
	}
}

func TestFilesReview_APathKeepsTheSpacesOfItsKey(t *testing.T) {
	o := FileObservation{Key: "/c ", Path: "/c ", Name: "c "}
	if err := o.Check(); err != nil || o.Path != o.Key {
		t.Fatalf("path %q vs key %q (%v): `files open` would look for the wrong file", o.Path, o.Key, err)
	}
}

// A file the client can't describe is still there: reported as kept, it isn't
// marked gone by the sync's finish, and a kept key for a file the vault has
// never seen adds nothing.
func TestFilesReview_AKeptFileIsNotMarkedGone(t *testing.T) {
	v, _ := tmpVault(t)
	src := fileSource(t, v)
	v.UpsertFiles(src, []FileObservation{obs("a", "/a.pdf", "a.pdf"), obs("b", "/b.pdf", "b.pdf")}, true)
	if _, err := v.UpsertFileBatch(src, "sync-kkkk-1", true, []FileObservation{
		obs("a", "/a.pdf", "a.pdf"), {Key: "b", Keep: true}, {Key: "never-seen", Keep: true},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := v.FinishFileSync(nil, src, "sync-kkkk-1", false)
	if err != nil || res.Gone != 0 {
		t.Fatalf("finish = %+v, %v", res, err)
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{}); total != 2 {
		t.Fatalf("%d files listed, want 2", total)
	}
	// A keep with no key is still refused.
	if _, err := v.UpsertFiles(src, []FileObservation{{Keep: true}}, false); err == nil {
		t.Fatal("a keep without a key")
	}
}
