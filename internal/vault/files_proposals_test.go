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
		v.ProposeFile(owner, driveProposal(fmt.Sprint("f", i), fmt.Sprintf("f%d.pdf", i)), "claude")
	}
	all, _ := v.FileProposals(nil)
	if len(all) != maxPendingSuggestions {
		t.Fatalf("%d pending, cap %d", len(all), maxPendingSuggestions)
	}
	v.ResolveFileProposal(nil, all[0].ID, false)
	if _, err := v.ProposeFile(owner, driveProposal("late", "late.pdf"), "claude"); err != nil {
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
