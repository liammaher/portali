package filesync

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keys(s *Scan) string {
	var out []string
	for _, f := range s.Files {
		out = append(out, f.Key)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestWalk_IndexesRegularFilesByRelativePath(t *testing.T) {
	root, _ := ResolveRoot(t.TempDir())
	write(t, filepath.Join(root, "Clienti", "Rossi", "visura.pdf"), "pdf")
	write(t, filepath.Join(root, "contratto.docx"), "docx")
	write(t, filepath.Join(root, "Società", "bilancio è.xlsx"), "x")

	s := Walk(root)
	if !s.Complete() {
		t.Fatalf("errors: %v", s.Errors)
	}
	if got := keys(s); got != "/Clienti/Rossi/visura.pdf,/Società/bilancio è.xlsx,/contratto.docx" {
		t.Fatalf("keys = %s", got)
	}
	for _, f := range s.Files {
		if f.Key != f.Path || f.Size == 0 || f.Mtime == "" {
			t.Fatalf("observation = %+v", f)
		}
		if f.Name == "visura.pdf" && f.MIME != "application/pdf" {
			t.Fatalf("mime = %q", f.MIME)
		}
	}
}

func TestWalk_LeavesOutHiddenLinksAndLockFiles(t *testing.T) {
	root, _ := ResolveRoot(t.TempDir())
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.txt"), "not in the root")
	write(t, filepath.Join(root, "ok.pdf"), "x")
	write(t, filepath.Join(root, ".hidden.pdf"), "x")
	write(t, filepath.Join(root, ".git", "config"), "x")
	write(t, filepath.Join(root, "~$ok.docx"), "x")
	write(t, filepath.Join(root, "Thumbs.db"), "x")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Fatal(err)
	}

	s := Walk(root)
	if got := keys(s); got != "/ok.pdf" {
		t.Fatalf("keys = %s", got)
	}
	if s.Skipped < 5 {
		t.Fatalf("skipped = %d", s.Skipped)
	}
}

func TestWalk_NamesTheVaultWouldRefuseAreSkippedNotFatal(t *testing.T) {
	root, _ := ResolveRoot(t.TempDir())
	write(t, filepath.Join(root, "ok.pdf"), "x")
	write(t, filepath.Join(root, "bell\x07.pdf"), "x")
	write(t, filepath.Join(root, "   "), "x")
	write(t, filepath.Join(root, "a\u202egpj.exe"), "x")
	write(t, filepath.Join(root, "back\\slash.pdf"), "x")
	s := Walk(root)
	if got := keys(s); got != "/ok.pdf" || s.Skipped != 4 {
		t.Fatalf("keys = %s, skipped = %d", got, s.Skipped)
	}
}

func TestWalk_UnreadableFolderMakesTheScanIncomplete(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	root, _ := ResolveRoot(t.TempDir())
	write(t, filepath.Join(root, "open", "a.pdf"), "x")
	write(t, filepath.Join(root, "closed", "b.pdf"), "x")
	closed := filepath.Join(root, "closed")
	os.Chmod(closed, 0)
	t.Cleanup(func() { os.Chmod(closed, 0o755) })

	s := Walk(root)
	if s.Complete() {
		t.Fatal("a scan that couldn't read a folder must not be complete: its files would be marked gone")
	}
	if got := keys(s); got != "/open/a.pdf" {
		t.Fatalf("keys = %s", got)
	}
}

func TestResolveRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	write(t, file, "x")
	if _, err := ResolveRoot(file); err == nil {
		t.Fatal("a file is not a root")
	}
	if _, err := ResolveRoot(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing folder is not a root")
	}
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(dir, link)
	got, err := ResolveRoot(link)
	want, _ := filepath.EvalSymlinks(dir)
	if err != nil || got != want {
		t.Fatalf("ResolveRoot(link) = %q, %v; want %q", got, err, want)
	}
}

func TestDetectProvider(t *testing.T) {
	for path, want := range map[string][2]string{
		"/Users/x/Library/CloudStorage/OneDrive-Acme":               {"onedrive", "OneDrive-Acme"},
		"/Users/x/Library/CloudStorage/OneDrive-Personal/Documenti": {"onedrive", "OneDrive-Personal"},
		"/Users/x/Library/CloudStorage/GoogleDrive-a@b.it/My Drive": {"gdrive", "GoogleDrive-a@b.it"},
		"/Users/x/Library/CloudStorage/Dropbox":                     {"dropbox", "Dropbox"},
		"/Users/x/Library/CloudStorage/SharePoint-Studio - Clienti": {"sharepoint", "SharePoint-Studio - Clienti"},
		"/Users/x/Dropbox":                         {"dropbox", "Dropbox"},
		"/Users/x/Documents/clienti":               {"", ""},
		"/Users/x/Library/CloudStorage/Box-Box":    {"", ""},
		"/Users/x/Desktop/OneDriveBackup-notes/..": {"", ""},
	} {
		p, a, ok := DetectProvider(path)
		if p != want[0] || a != want[1] || ok != (want[0] != "") {
			t.Errorf("DetectProvider(%q) = %q, %q, %v; want %v", path, p, a, ok, want)
		}
	}
}
