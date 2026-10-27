package vault

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func driveProposal(key, name string) FileProposalInput {
	return FileProposalInput{
		Provider: "gdrive", Key: key, Name: name, Path: "/Documenti/" + name, MIME: "application/pdf", Size: 1234,
		Mtime: "2026-10-01T10:00:00.000Z", WebURL: "https://drive.google.com/file/d/" + key + "/view?usp=drivesdk",
	}
}

func TestProposals_NothingIsIndexedUntilTheUserAccepts(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	agent := tokenFilter("*:files.*")

	in := driveProposal("1AbC", "CIE Leo.pdf")
	in.Holder, in.Role = "leo", "cie"
	in.Reason = "found by searching Drive for Leo's CIE"
	p, err := v.ProposeFile(agent, in, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if p.ProposedBy != "claude" || p.Kind != FileKindPerson {
		t.Fatalf("proposal = %+v", p)
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{IncludeGone: true}); total != 0 {
		t.Fatal("a proposal entered the index")
	}
	if srcs, _ := v.FileSources(nil); len(srcs) != 0 {
		t.Fatalf("a proposal made a source: %+v", srcs)
	}
	// The same file again is the same proposal.
	again, err := v.ProposeFile(agent, in, "claude")
	if err != nil || again.ID != p.ID {
		t.Fatalf("again = %+v, %v", again, err)
	}
	if list, _ := v.FileProposals(nil); len(list) != 1 || list[0].Reason != in.Reason {
		t.Fatalf("pending = %+v", list)
	}

	// Nothing the proposal holds is in a plain column.
	rows, _ := v.db.ListFileProposals()
	for _, r := range rows {
		if strings.Contains(r.Blob, "CIE") || strings.Contains(r.Blob, "1AbC") || strings.Contains(r.Blob, "Documenti") {
			t.Fatal("a proposal is stored in the clear")
		}
	}

	f, err := v.ResolveFileProposal(nil, p.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "CIE Leo.pdf" || len(f.Links) != 1 || f.Links[0].Holder != "leo" || f.Links[0].Role != "cie" || f.Links[0].Origin != FileOriginSuggestion {
		t.Fatalf("accepted file = %+v", f)
	}
	srcs, _ := v.FileSources(nil)
	if len(srcs) != 1 || srcs[0].Via != ViaConnector || srcs[0].Provider != "gdrive" || srcs[0].Root != "" || srcs[0].ClientID != "" || srcs[0].Files != 1 {
		t.Fatalf("sources = %+v", srcs)
	}
	if left, _ := v.FileProposals(nil); len(left) != 0 {
		t.Fatal("an accepted proposal is still pending")
	}
	if _, err := v.ResolveFileProposal(nil, p.ID, true); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("accepting twice = %v", err)
	}

	// A second file of the same provider goes to the same source; a rejected one nowhere.
	p2, _ := v.ProposeFile(agent, driveProposal("2DeF", "Contratto.pdf"), "claude")
	p3, _ := v.ProposeFile(agent, driveProposal("3GhI", "Altro.pdf"), "claude")
	if _, err := v.ResolveFileProposal(nil, p2.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ResolveFileProposal(nil, p3.ID, false); err != nil {
		t.Fatal(err)
	}
	if srcs, _ := v.FileSources(nil); len(srcs) != 1 || srcs[0].Files != 2 {
		t.Fatalf("sources after two = %+v", srcs)
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{Text: "altro"}); total != 0 {
		t.Fatal("a rejected proposal is in the index")
	}
}

func TestProposals_AgentInputIsHeldToWhatTheIndexKeeps(t *testing.T) {
	v, _ := tmpVault(t)
	agent := tokenFilter("files.*")
	var verr *ValidationError
	for name, in := range map[string]FileProposalInput{
		"unknown provider":      {Provider: "box", Key: "k", Name: "a.pdf"},
		"no key":                {Provider: "gdrive", Name: "a.pdf"},
		"no name":               {Provider: "gdrive", Key: "k"},
		"control in the name":   {Provider: "gdrive", Key: "k", Name: "a\x07.pdf"},
		"direction override":    {Provider: "gdrive", Key: "k", Name: "a‮gpj.exe"},
		"a phishing address":    {Provider: "gdrive", Key: "k", Name: "a.pdf", WebURL: "https://drive.google.com.evil.example/file/d/k/view"},
		"another provider's":    {Provider: "gdrive", Key: "k", Name: "a.pdf", WebURL: "https://www.dropbox.com/home/a.pdf"},
		"plain http":            {Provider: "gdrive", Key: "k", Name: "a.pdf", WebURL: "http://drive.google.com/file/d/k/view"},
		"a pratica that's none": {Provider: "gdrive", Key: "k", Name: "a.pdf", Pratica: "nessuna"},
		"a person that's none":  {Provider: "gdrive", Key: "k", Name: "a.pdf", Holder: "ghost"},
	} {
		if _, err := v.ProposeFile(tokenFilter("*:files.*"), in, "claude"); !errors.As(err, &verr) {
			t.Errorf("%s: %v, want a ValidationError", name, err)
		}
	}
	// A sharing link costs the proposal its address, not the file.
	in := driveProposal("k", "a.pdf")
	in.WebURL = "https://drive.google.com/file/d/k/view?usp=sharing"
	p, err := v.ProposeFile(agent, in, "claude")
	if err != nil || p.WebURL != "" {
		t.Fatalf("sharing link: %+v, %v", p, err)
	}
	// A pratica the token can't read looks like one that doesn't exist.
	mustSet(t, v, "pratiche.tari-2026_stato", "in_preparazione")
	in = driveProposal("k2", "b.pdf")
	in.Pratica = "tari-2026"
	_, errHidden := v.ProposeFile(agent, in, "claude")
	in.Pratica = "nessuna"
	_, errMissing := v.ProposeFile(agent, in, "claude")
	if errHidden == nil || errMissing == nil || strings.ReplaceAll(errHidden.Error(), "tari-2026", "X") != strings.ReplaceAll(errMissing.Error(), "nessuna", "X") {
		t.Fatalf("hidden %v / missing %v", errHidden, errMissing)
	}
}

func TestProposals_HeldToTheCallersScopeAndBounded(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	owner := tokenFilter("files.*")

	// A token for the owner's files can't propose a file for Leo, or see his proposals.
	in := driveProposal("1", "a.pdf")
	in.Holder = "leo"
	if _, err := v.ProposeFile(owner, in, "claude"); err == nil {
		t.Fatal("a proposal for a holder outside the token's scope")
	}
	if _, err := v.ProposeFile(tokenFilter("leo:files.*"), in, "claude"); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.FileProposals(owner); len(got) != 0 {
		t.Fatalf("the owner scope sees leo's proposals: %+v", got)
	}
	// And can't decide on them.
	list, _ := v.FileProposals(nil)
	if _, err := v.ResolveFileProposal(owner, list[0].ID, true); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("owner scope accepting leo's proposal: %v", err)
	}
	// A proposal with no link needs the owner's scope.
	if _, err := v.ProposeFile(tokenFilter("leo:files.*"), driveProposal("2", "b.pdf"), "claude"); err == nil {
		t.Fatal("a proposal for the owner by a token that only reaches leo")
	}

	// The queue is bounded, and deciding makes room.
	for i := 0; i < maxPendingSuggestions+5; i++ {
		v.ProposeFile(owner, driveProposal(fmt.Sprint("f", i), fmt.Sprintf("f%d.pdf", i)), fmt.Sprint("agent-", i%6))
	}
	all, _ := v.FileProposals(nil)
	if len(all) != maxPendingSuggestions {
		t.Fatalf("%d pending, cap %d", len(all), maxPendingSuggestions)
	}
	v.ResolveFileProposal(nil, all[0].ID, false)
	if _, err := v.ProposeFile(owner, driveProposal("late", "late.pdf"), "agent-late"); err != nil {
		t.Fatalf("after room was made: %v", err)
	}
}

func TestProposals_RemovingAPersonDropsTheirProposals(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	in := driveProposal("1", "a.pdf")
	in.Holder = "leo"
	if _, err := v.ProposeFile(tokenFilter("*:files.*"), in, "claude"); err != nil {
		t.Fatal(err)
	}
	out, err := v.RemovePerson("leo")
	if err != nil {
		t.Fatal(err)
	}
	if out.FileLinks != 1 {
		t.Fatalf("removal counted %d", out.FileLinks)
	}
	if got, _ := v.FileProposals(nil); len(got) != 0 {
		t.Fatalf("a removed person's proposal remains: %+v", got)
	}
}

func TestProposals_SourceConfigRules(t *testing.T) {
	v, _ := tmpVault(t)
	var verr *ValidationError
	for _, cfg := range []FileSourceConfig{
		{Label: "x", Via: "connector", Root: "/tmp"},
		{Label: "x", Via: "connector", ClientID: "c"},
		{Label: "x", Via: "magic"},
	} {
		if _, err := v.AddFileSource("gdrive", cfg); !errors.As(err, &verr) {
			t.Errorf("%+v: %v", cfg, err)
		}
	}
	if _, err := v.AddFileSource("local", FileSourceConfig{Label: "x", Via: "connector"}); !errors.As(err, &verr) {
		t.Errorf("a local source fed by an agent: %v", err)
	}
	if _, err := v.AddFileSource("gdrive", FileSourceConfig{Label: "x", Via: "connector"}); err != nil {
		t.Errorf("a connector source: %v", err)
	}
}

// "This is the owner's CIE" is a request for a link even though the owner is
// the empty holder.
func TestProposals_AnOwnerLinkIsKept(t *testing.T) {
	v, _ := tmpVault(t)
	in := driveProposal("1", "CIE.pdf")
	in.Role = "cie"
	p, err := v.ProposeFile(tokenFilter("files.*"), in, "claude")
	if err != nil {
		t.Fatal(err)
	}
	f, err := v.ResolveFileProposal(nil, p.ID, true)
	if err != nil || len(f.Links) != 1 || f.Links[0].Holder != "" || f.Links[0].Role != "cie" {
		t.Fatalf("file = %+v, %v", f, err)
	}
	// In a business vault the owner is the business: the same proposal links to it.
	b := tmpVaultKind(t, KindImpresa)
	pb, err := b.ProposeFile(tokenFilter("files.*"), in, "claude")
	if err != nil {
		t.Fatalf("business proposal: %v", err)
	}
	fb, err := b.ResolveFileProposal(nil, pb.ID, true)
	if err != nil || len(fb.Links) != 1 || fb.Links[0].Kind != FileKindBusiness || fb.Links[0].Role != "cie" {
		t.Fatalf("business file = %+v, %v", fb, err)
	}
}

// What a second token learns of another token's proposal: that it exists, not what it says.
func TestProposals_ADuplicateDoesNotLeakTheFirst(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	in := driveProposal("K1", "CIE Leo.pdf")
	in.Holder, in.Role, in.Reason = "leo", "cie", "a secret reason"
	if _, err := v.ProposeFile(tokenFilter("*:files.*"), in, "token-a"); err != nil {
		t.Fatal(err)
	}
	other := driveProposal("K1", "x.pdf")
	got, err := v.ProposeFile(tokenFilter("files.*"), other, "token-b")
	if got != nil || err == nil || strings.Contains(err.Error(), "CIE") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("a token that can't see leo's proposal got %+v, %v", got, err)
	}
	// One that can see it gets it back, and no second row is made.
	again, err := v.ProposeFile(tokenFilter("*:files.*"), in, "token-c")
	if err != nil || again.ProposedBy != "token-a" {
		t.Fatalf("again = %+v, %v", again, err)
	}
	if list, _ := v.FileProposals(nil); len(list) != 1 {
		t.Fatalf("%d proposals", len(list))
	}
}

// Accepting is all or nothing: a link that can no longer be made leaves the
// index as it was, and the file isn't left behind as the owner's.
func TestProposals_AFailedLinkLeavesNoStrayFile(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "pratiche.tari-2026_stato", "in_preparazione")
	in := driveProposal("1", "a.pdf")
	in.Holder = "leo"
	p1, _ := v.ProposeFile(tokenFilter("*:files.*"), in, "claude")
	in2 := driveProposal("2", "b.pdf")
	in2.Pratica = "tari-2026"
	p2, _ := v.ProposeFile(tokenFilter("*:files.*,pratiche.*"), in2, "claude")

	// The person goes (this removes their proposal too); the pratica's field is deleted.
	if _, err := v.RemovePerson("leo"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ResolveFileProposal(nil, p1.ID, true); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("a removed person's proposal = %v", err)
	}
	if err := v.Delete("pratiche.tari-2026_stato"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ResolveFileProposal(nil, p2.ID, true); err == nil || !strings.Contains(err.Error(), "reject it") {
		t.Fatalf("accepting a link to a pratica that is gone = %v", err)
	}
	if _, total, _ := v.SearchFiles(nil, FileQuery{IncludeGone: true}); total != 0 {
		t.Fatalf("%d files in the index after a failed accept", total)
	}
	if srcs, _ := v.FileSources(nil); len(srcs) != 0 {
		t.Fatalf("a failed accept made a source: %+v", srcs)
	}
	// The user can still reject it.
	if _, err := v.ResolveFileProposal(nil, p2.ID, false); err != nil {
		t.Fatal(err)
	}
}

func TestProposals_PerTokenCapAndReplaces(t *testing.T) {
	v, _ := tmpVault(t)
	agent := tokenFilter("files.*")
	for i := 0; i < maxPendingPerProposer+3; i++ {
		v.ProposeFile(agent, driveProposal(fmt.Sprint("a", i), fmt.Sprintf("a%d.pdf", i)), "token-a")
	}
	if list, _ := v.FileProposals(nil); len(list) != maxPendingPerProposer {
		t.Fatalf("%d pending from one token, cap %d", len(list), maxPendingPerProposer)
	}
	// Another token still has room.
	if _, err := v.ProposeFile(agent, driveProposal("b1", "b.pdf"), "token-b"); err != nil {
		t.Fatalf("another token: %v", err)
	}

	// A file accepted before and proposed again says it replaces its entry.
	list, _ := v.FileProposals(nil)
	var first FileProposalInfo
	for _, p := range list {
		if p.Key == "a0" {
			first = p
		}
	}
	v.ResolveFileProposal(nil, first.ID, true)
	again := driveProposal("a0", "a0 renamed.pdf")
	if _, err := v.ProposeFile(agent, again, "token-b"); err != nil {
		t.Fatal(err)
	}
	for _, p := range func() []FileProposalInfo { l, _ := v.FileProposals(nil); return l }() {
		if p.Key == "a0" && p.Replaces != "a0.pdf" {
			t.Fatalf("replaces = %q", p.Replaces)
		}
		if p.Key == "b1" && p.Replaces != "" {
			t.Fatalf("a new file says it replaces %q", p.Replaces)
		}
	}
}

func TestProposals_GoogleAddressesAreTheFilesOwnPage(t *testing.T) {
	v, _ := tmpVault(t)
	agent := tokenFilter("files.*")
	for _, u := range []string{
		"https://drive.google.com/uc?export=download&id=K9",
		"https://docs.google.com/forms/d/e/1FAIpQ/viewform",
		"https://drive.google.com/file/d/SOMEONEELSE/view",
	} {
		in := driveProposal("K9", "a.pdf")
		in.WebURL = u
		if _, err := v.ProposeFile(agent, in, "claude"); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
	in := driveProposal("K9", "a.pdf")
	in.WebURL = "https://docs.google.com/document/d/K9/edit"
	if _, err := v.ProposeFile(agent, in, "claude"); err != nil {
		t.Errorf("a Doc's own page: %v", err)
	}
}

// A source an agent feeds is never read by a sync, so a sync token can't
// rewrite or tombstone what the user accepted.
func TestProposals_ASyncCannotTouchAConnectorSource(t *testing.T) {
	v, _ := tmpVault(t)
	p, _ := v.ProposeFile(tokenFilter("files.*"), driveProposal("1", "a.pdf"), "claude")
	v.ResolveFileProposal(nil, p.ID, true)
	srcs, _ := v.FileSources(nil)
	id := srcs[0].ID
	for name, f := range map[string]func() error{
		"a full listing": func() error { _, err := v.UpsertFilesIf(tokenFilter("files.*"), id, nil, true); return err },
		"a new file": func() error {
			_, err := v.UpsertFiles(id, []FileObservation{obs("x", "/x.pdf", "x.pdf")}, false)
			return err
		},
		"a deletion": func() error {
			_, err := v.UpsertFiles(id, []FileObservation{{Key: "1", Deleted: true}}, false)
			return err
		},
		"a batched sync": func() error { _, err := v.UpsertFileBatch(id, "sync-cccc-9", true, nil); return err },
	} {
		var verr *ValidationError
		if err := f(); !errors.As(err, &verr) {
			t.Errorf("%s: %v, want a ValidationError", name, err)
		}
	}
	if got, _, _ := v.SearchFiles(nil, FileQuery{}); len(got) != 1 || got[0].Gone {
		t.Fatalf("the accepted file changed: %+v", got)
	}
}

// Linking again with a role says more than the first link did.
func TestFiles_ARelinkWithARoleKeepsTheRole(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	src := fileSource(t, v)
	v.AddFileRule(src, FileRuleInput{Path: "/Leo", Holder: "leo", Kind: FileKindPerson})
	v.UpsertFiles(src, []FileObservation{obs("1", "/Leo/cie.pdf", "cie.pdf")}, true)
	f, _, _ := v.SearchFiles(nil, FileQuery{})
	if f[0].Links[0].Role != "" || f[0].Links[0].Origin != FileOriginRule {
		t.Fatalf("rule link = %+v", f[0].Links)
	}
	l, err := v.LinkFile(f[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson, Role: "cie"}, FileOriginManual)
	if err != nil || l.Role != "cie" {
		t.Fatalf("relink = %+v, %v", l, err)
	}
	f, _, _ = v.SearchFiles(nil, FileQuery{})
	if len(f[0].Links) != 1 || f[0].Links[0].Role != "cie" {
		t.Fatalf("after relinking: %+v", f[0].Links)
	}
	// A relink without a role doesn't blank it.
	v.LinkFile(f[0].ID, FileLinkInput{Holder: "leo", Kind: FileKindPerson}, FileOriginManual)
	f, _, _ = v.SearchFiles(nil, FileQuery{})
	if f[0].Links[0].Role != "cie" {
		t.Fatalf("a bare relink lost the role: %+v", f[0].Links)
	}
}
