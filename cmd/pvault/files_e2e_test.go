package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fileID finds the ID printed for name in search output.
func fileID(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && len(f[0]) == 32 && f[1] == name {
			return f[0]
		}
	}
	t.Fatalf("no %q in:\n%s", name, out)
	return ""
}

// TestFiles_EndToEnd indexes a folder, links files by hand and by rule,
// follows a rename, tombstones a deletion, and refuses to tombstone from an
// incomplete scan, all through the CLI with Touch ID grants.
func TestFiles_EndToEnd(t *testing.T) {
	v, argsFile := touchIDVault(t, map[string]string{"leo:famiglia.relazione": "figlio"})
	_ = v
	root := filepath.Join(t.TempDir(), "CloudStorage", "OneDrive-Acme")
	writeFile(t, filepath.Join(root, "Casa", "contratto.pdf"), "x")
	writeFile(t, filepath.Join(root, "Leo", "scuola.pdf"), "x")
	writeFile(t, filepath.Join(root, "Leo", "ricevute", "mensa.pdf"), "x")
	writeFile(t, filepath.Join(root, ".nascosto.pdf"), "x")

	out := runCmd(t, cmdFiles, "files", "source", "add", root)
	if !strings.Contains(out, "OneDrive-Acme") || !strings.Contains(out, "(onedrive)") {
		t.Fatalf("source add didn't recognise the sync folder:\n%s", out)
	}
	out = runCmd(t, cmdFiles, "files", "sync")
	if !strings.Contains(out, "3 files") || !strings.Contains(out, "3 new") || !strings.Contains(out, "1 hidden") {
		t.Fatalf("first sync:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "sync", "onedrive-acme"); !strings.Contains(out, "3 unchanged") {
		t.Fatalf("second sync:\n%s", out)
	}

	// A rule links a folder now and later.
	out = runCmd(t, cmdFiles, "files", "rule", "add", "onedrive-acme", "--path", "/Leo", "--for", "leo")
	if !strings.Contains(out, "Linked 2 existing") {
		t.Fatalf("rule add:\n%s", out)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--scope leo:files.index,files.index --access write") || strings.Contains(string(args), "--scope *") {
		t.Errorf("a rule for leo should ask for leo's files and the owner's, not everything:\n%s", args)
	}
	writeFile(t, filepath.Join(root, "Leo", "pagella.pdf"), "x")
	if out := runCmd(t, cmdFiles, "files", "sync"); !strings.Contains(out, "1 new") || !strings.Contains(out, "1 linked by rules") {
		t.Fatalf("sync after a new file in a ruled folder:\n%s", out)
	}

	// Manual link, then search finds it by pratica and by person.
	v.Set("pratiche.tari-2026_stato", "in_preparazione", "")
	out = runCmd(t, cmdFiles, "files", "search", "contratto")
	id := fileID(t, out, "contratto.pdf")
	if !strings.Contains(out, "not linked") {
		t.Fatalf("search:\n%s", out)
	}
	runCmd(t, cmdFiles, "files", "link", id, "--pratica", "tari-2026", "--role", "contratto")
	out = runCmd(t, cmdFiles, "files", "search", "--pratica", "tari-2026")
	if !strings.Contains(out, "contratto.pdf") || !strings.Contains(out, "pratica tari-2026 (contratto)") {
		t.Fatalf("search by pratica:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "search", "--for", "leo"); strings.Contains(out, "contratto") || !strings.Contains(out, "mensa.pdf") {
		t.Fatalf("search for leo:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "search", "--unlinked"); !strings.Contains(out, "No files found") {
		t.Fatalf("unlinked:\n%s", out)
	}

	// A rename keeps the link? A local file's key is its path, so a move is
	// a new file and the old one goes: the links stay on the old entry (the
	// user's record) and the folder rule links the new one.
	if err := os.Rename(filepath.Join(root, "Leo", "scuola.pdf"), filepath.Join(root, "Leo", "scuola 2026.pdf")); err != nil {
		t.Fatal(err)
	}
	out = runCmd(t, cmdFiles, "files", "sync")
	if !strings.Contains(out, "1 new") || !strings.Contains(out, "1 gone") {
		t.Fatalf("sync after a rename:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "search", "scuola", "--gone"); !strings.Contains(out, "[gone]") || !strings.Contains(out, "scuola 2026.pdf") {
		t.Fatalf("gone listing:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "search", "scuola"); strings.Contains(out, "[gone]") || !strings.Contains(out, "linked to leo") {
		t.Fatalf("live listing:\n%s", out)
	}
	// Prune keeps the gone file: it has a link.
	if out := runCmd(t, cmdFiles, "files", "prune"); !strings.Contains(out, "Dropped 0") {
		t.Fatalf("prune:\n%s", out)
	}

	// An unreadable folder makes a scan incomplete: nothing is marked gone,
	// even for the files that did vanish from the part that was read.
	if os.Geteuid() != 0 {
		os.Remove(filepath.Join(root, "Casa", "contratto.pdf"))
		writeFile(t, filepath.Join(root, "Chiusa", "x.pdf"), "x")
		closed := filepath.Join(root, "Chiusa")
		os.Chmod(closed, 0)
		t.Cleanup(func() { os.Chmod(closed, 0o755) })
		out = runCmd(t, cmdFiles, "files", "sync")
		if !strings.Contains(out, "incomplete") || !strings.Contains(out, "0 gone") {
			t.Fatalf("incomplete scan:\n%s", out)
		}
		os.Chmod(closed, 0o755)
	}

	// An empty or missing folder never wipes the index.
	moved := root + ".away"
	os.Rename(root, moved)
	out = runCmd(t, cmdFiles, "files", "sync")
	if !strings.Contains(out, "Nothing was marked gone") {
		t.Fatalf("missing folder:\n%s", out)
	}
	os.Mkdir(root, 0o755)
	out = runCmd(t, cmdFiles, "files", "sync")
	if !strings.Contains(out, "not touching the index") {
		t.Fatalf("empty folder:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "search", "mensa"); !strings.Contains(out, "mensa.pdf") || strings.Contains(out, "[gone]") {
		t.Fatalf("index after the empty folder:\n%s", out)
	}

	// Forgetting a source removes its entries.
	out = runCmd(t, cmdFiles, "files", "source", "remove", "onedrive-acme")
	if !strings.Contains(out, "Forgot OneDrive-Acme") {
		t.Fatalf("remove:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "search"); !strings.Contains(out, "No files found") {
		t.Fatalf("after forget:\n%s", out)
	}
}

func TestFiles_LocalPathOfStaysInsideTheSource(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(root, "a", "b.pdf"), "x")
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret"), "x")
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link"))

	if p, err := localPathOf(root, "/a/b.pdf"); err != nil || p != filepath.Join(root, "a", "b.pdf") {
		t.Fatalf("inside: %q, %v", p, err)
	}
	for _, rel := range []string{"/../" + filepath.Base(outside) + "/secret", "/link", "/a", "/missing.pdf"} {
		if p, err := localPathOf(root, rel); err == nil {
			t.Errorf("localPathOf(%q) = %q, want an error", rel, p)
		}
	}
}

func TestFiles_BoolFlags(t *testing.T) {
	rest, set, err := splitBoolFlags([]string{"visura", "--gone", "--for", "leo"}, "--gone", "--unlinked")
	if err != nil || !set["--gone"] || set["--unlinked"] || strings.Join(rest, " ") != "visura --for leo" {
		t.Fatalf("%v %v %v", rest, set, err)
	}
	if _, _, err := splitBoolFlags([]string{"--gone", "--gone"}, "--gone"); err == nil {
		t.Fatal("a repeated flag should be an error")
	}
}

func TestFiles_BatchesStayUnderTheServerLimits(t *testing.T) {
	var files []vault.FileObservation
	for i := 0; i < 1000; i++ {
		p := "/" + strings.Repeat("è", 400) + fmt.Sprint(i) // long, and 6 bytes a character as JSON
		files = append(files, vault.FileObservation{Key: p, Path: p, Name: "f.pdf"})
	}
	total := 0
	for _, b := range batches(files) {
		raw, _ := json.Marshal(map[string]any{"source_id": "x", "sync_id": "y", "begin": true, "files": b})
		if len(b) > syncBatch || len(raw) > 1<<20 {
			t.Fatalf("batch of %d files is %d bytes", len(b), len(raw))
		}
		total += len(b)
	}
	if total != len(files) {
		t.Fatalf("batches hold %d of %d files", total, len(files))
	}
	if n := len(batches(nil)); n != 0 {
		t.Fatalf("no files made %d batches", n)
	}
}

// A client's ID copy that sits in a folder is reported once their pratiche
// close: the vault can only point at it.
func TestFiles_ExpiringListsLinkedClientFiles(t *testing.T) {
	v, _ := touchIDVaultKind(t, vault.KindImpresa, map[string]string{"business.denominazione": "Studio Test"})
	verifiedKind = ""
	t.Cleanup(func() { verifiedKind = "" })
	root := filepath.Join(t.TempDir(), "Clienti")
	cie := filepath.Join(root, "Rossi", "cie-rossi.pdf")
	writeFile(t, cie, "x")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(cie, old, old); err != nil {
		t.Fatal(err)
	}
	// In this order: a pratica can only name a person who is already there.
	for _, kv := range [][2]string{
		{"rossi:ruolo.tipo", "cliente"}, {"pratiche.cila-rossi_persona", "rossi"}, {"pratiche.cila-rossi_stato", "completata"},
	} {
		if err := v.Set(kv[0], kv[1], ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.SetScanDays(0); err != nil {
		t.Fatal(err)
	}

	runCmd(t, cmdFiles, "files", "source", "add", root, "--label", "Clienti")
	runCmd(t, cmdFiles, "files", "sync")
	out := runCmd(t, cmdFiles, "files", "search", "cie")
	id := fileID(t, out, "cie-rossi.pdf")
	runCmd(t, cmdFiles, "files", "link", id, "--for", "rossi", "--kind", "client", "--role", "cie")

	out = runCmd(t, cmdExpiring, "expiring")
	for _, want := range []string{"cie file (rossi)", "delete the file unless still needed", "cie-rossi.pdf (" + id + ")", "files show " + id} {
		if !strings.Contains(out, want) {
			t.Fatalf("expiring lacks %q:\n%s", want, out)
		}
	}
}

// A file an agent found through its own connector is proposed, listed for the
// user, accepted, searchable and linked, and never touched by a sync.
func TestFiles_ProposalsEndToEnd(t *testing.T) {
	v, _ := touchIDVault(t, map[string]string{"leo:famiglia.relazione": "figlio"})
	in := vault.FileProposalInput{
		Provider: "gdrive", Key: "1AbC", Name: "CIE-Leo.pdf", Path: "/Documenti/CIE-Leo.pdf",
		WebURL: "https://drive.google.com/file/d/1AbC/view?usp=drivesdk", Holder: "leo", Role: "cie", Reason: "its name says it is Leo's CIE",
	}
	if _, err := v.ProposeFile(nil, in, "claude"); err != nil {
		t.Fatal(err)
	}

	out := runCmd(t, cmdFiles, "files", "proposals")
	for _, want := range []string{"claude proposes adding \"CIE-Leo.pdf\" from Google Drive", "/Documenti/CIE-Leo.pdf", "linking it to leo (cie)", `reason: "its name says`} {
		if !strings.Contains(out, want) {
			t.Fatalf("proposals lacks %q:\n%s", want, out)
		}
	}
	if out := runCmd(t, cmdFiles, "files", "search", "CIE"); !strings.Contains(out, "No files found") {
		t.Fatalf("a proposal is searchable:\n%s", out)
	}
	pid := strings.Fields(runCmd(t, cmdFiles, "files", "proposals"))[0]
	out = runCmd(t, cmdFiles, "files", "proposals", "accept", pid)
	if !strings.Contains(out, "Added to the index: CIE-Leo.pdf") {
		t.Fatalf("accept:\n%s", out)
	}
	out = runCmd(t, cmdFiles, "files", "search", "--for", "leo")
	if !strings.Contains(out, "CIE-Leo.pdf") || !strings.Contains(out, "linked to leo (cie)") || !strings.Contains(out, "found through Claude") {
		t.Fatalf("after accepting:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "proposals"); !strings.Contains(out, "No proposals waiting") {
		t.Fatalf("still pending:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "source", "list"); !strings.Contains(out, "never synced") || !strings.Contains(out, "found through its own connector") {
		t.Fatalf("source list:\n%s", out)
	}
	// A sync has nothing to read there, and leaves its file alone.
	if out := runCmd(t, cmdFiles, "files", "sync"); !strings.Contains(out, "Nothing to sync") {
		t.Fatalf("sync:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "search", "CIE"); !strings.Contains(out, "CIE-Leo.pdf") || strings.Contains(out, "[gone]") {
		t.Fatalf("after a sync:\n%s", out)
	}

	// The address of a file an agent proposed is shown, never opened by pvault.
	fid := fileID(t, runCmd(t, cmdFiles, "files", "search", "CIE"), "CIE-Leo.pdf")
	if out := runCmd(t, cmdFiles, "files", "open", fid); !strings.Contains(out, "https://drive.google.com/file/d/1AbC/view") || !strings.Contains(out, "check it before you open it") {
		t.Fatalf("open:\n%s", out)
	}

	// Rejecting forgets it.
	in.Key, in.Name, in.Path = "2DeF", "Altro.pdf", "/Altro.pdf"
	in.Holder, in.Role, in.WebURL = "", "", "" // (an address must carry the file's own id)
	if _, err := v.ProposeFile(nil, in, "claude"); err != nil {
		t.Fatal(err)
	}
	runCmd(t, cmdFiles, "files", "proposals", "reject", strings.Fields(runCmd(t, cmdFiles, "files", "proposals"))[0])
	if out := runCmd(t, cmdFiles, "files", "search", "altro"); !strings.Contains(out, "No files found") {
		t.Fatalf("a rejected proposal:\n%s", out)
	}

	// A queue an agent filled is emptied in one command; a file accepted before is flagged as replaced.
	for i := 0; i < 3; i++ {
		in.Key, in.Name, in.Path = fmt.Sprint("junk", i), fmt.Sprintf("junk%d.pdf", i), fmt.Sprintf("/junk%d.pdf", i)
		in.WebURL = ""
		v.ProposeFile(nil, in, "claude")
	}
	in.Key, in.Name, in.Path = "1AbC", "CIE-Leo-renamed.pdf", "/Documenti/CIE-Leo-renamed.pdf"
	in.WebURL = "https://drive.google.com/file/d/1AbC/view?usp=drivesdk"
	v.ProposeFile(nil, in, "claude")
	if out := runCmd(t, cmdFiles, "files", "proposals"); !strings.Contains(out, "REPLACES the entry already in the index") || !strings.Contains(out, `id "1AbC"`) {
		t.Fatalf("proposals:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "proposals", "reject", "--all"); !strings.Contains(out, "Rejected 4 proposal(s)") {
		t.Fatalf("reject --all:\n%s", out)
	}
	if out := runCmd(t, cmdFiles, "files", "proposals"); !strings.Contains(out, "No proposals waiting") {
		t.Fatalf("after reject --all:\n%s", out)
	}
}
