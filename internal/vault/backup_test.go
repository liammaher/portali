package vault

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// backupVault returns an unlocked vault with a field, a personal field and a
// document, backing up to a fresh folder.
func backupVault(t *testing.T) (*Vault, string, string) {
	t.Helper()
	v, sk := tmpVault(t)
	if err := v.Set("notes.motto", "carpe diem", ""); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("notes.secret_word", "pizza", TierPersonal); err != nil {
		t.Fatal(err)
	}
	if _, err := v.AddDocument("passaporto", "scan.pdf", "application/pdf", []byte("passport scan"), ""); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := v.SetBackupDir(dir); err != nil {
		t.Fatal(err)
	}
	return v, sk, dir
}

// backedUpVault is backupVault plus one backup, returning its path.
func backedUpVault(t *testing.T) (*Vault, string, string) {
	t.Helper()
	v, sk, _ := backupVault(t)
	path, err := v.Backup()
	if err != nil {
		t.Fatal(err)
	}
	return v, sk, path
}

func backupFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if reBackupName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	_, sk, path := backedUpVault(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("backup mode %v, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	for _, plain := range []string{"notes.motto", "passaporto", "SQLite format"} {
		if bytes.Contains(data, []byte(plain)) {
			t.Fatalf("backup contains %q in the clear", plain)
		}
	}

	// The secret key may be typed as copied by hand.
	typed := strings.ToUpper(sk[:8]) + " " + sk[8:16] + "-" + sk[16:]
	check, err := CheckBackup(path, testPassword, typed)
	if err != nil {
		t.Fatal(err)
	}
	if check.Fields != 2 || check.Documents != 1 {
		t.Fatalf("check = %+v, want 2 fields and 1 document", check)
	}

	dir := filepath.Join(t.TempDir(), "restored")
	if _, err := RestoreBackup(path, dir+"/", testPassword, sk); err != nil {
		t.Fatal(err)
	}
	skFile, _ := os.ReadFile(filepath.Join(dir, "secret.key"))
	if strings.TrimSpace(string(skFile)) != sk {
		t.Fatal("restored secret.key does not match")
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Unlock(testPassword, typed); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"notes.motto": "carpe diem", "notes.secret_word": "pizza"} {
		f, err := r.Get(id)
		if err != nil || f == nil || f.Value != want {
			t.Fatalf("restored %s = %+v, %v", id, f, err)
		}
	}
	docs, err := r.ListDocuments(nil)
	if err != nil || len(docs) != 1 {
		t.Fatalf("restored documents = %v, %v", docs, err)
	}
	_, content, err := r.GetDocument(docs[0].ID)
	if err != nil || string(content) != "passport scan" {
		t.Fatalf("restored document = %q, %v", content, err)
	}
}

func TestBackupRejectsWrongCredentialsAndDamage(t *testing.T) {
	_, sk, path := backedUpVault(t)
	if _, err := CheckBackup(path, "wrong-password", sk); !errors.Is(err, ErrBackupUnreadable) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := CheckBackup(path, testPassword, strings.Repeat("ab", 16)); !errors.Is(err, ErrBackupUnreadable) {
		t.Fatalf("wrong secret key: %v", err)
	}

	data, _ := os.ReadFile(path)
	damaged := map[string][]byte{
		"flipped last byte": append(append([]byte{}, data[:len(data)-1]...), data[len(data)-1]^1),
		"truncated":         data[:len(data)-100],
		"salt changed":      append(append(append([]byte{}, data[:20]...), data[20]^1), data[21:]...),
	}
	for name, d := range damaged {
		p := filepath.Join(t.TempDir(), "b.pvbackup")
		os.WriteFile(p, d, 0600)
		if _, err := CheckBackup(p, testPassword, sk); !errors.Is(err, ErrBackupUnreadable) {
			t.Errorf("%s: expected ErrBackupUnreadable, got %v", name, err)
		}
	}
	notBackup := filepath.Join(t.TempDir(), "x")
	os.WriteFile(notBackup, []byte("hello"), 0600)
	if _, err := CheckBackup(notBackup, testPassword, sk); err == nil || !strings.Contains(err.Error(), "not a pvault backup") {
		t.Fatalf("non-backup file: %v", err)
	}
}

func TestRestoreNeverOverwrites(t *testing.T) {
	v, sk, path := backedUpVault(t)
	if _, err := RestoreBackup(path, v.dir, testPassword, sk); err == nil {
		t.Fatal("restore over an existing vault succeeded")
	}
	// A failed restore leaves nothing behind.
	dir := filepath.Join(t.TempDir(), "restored")
	if _, err := RestoreBackup(path, dir, "wrong-password", sk); err == nil {
		t.Fatal("restore with wrong password succeeded")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed restore left %s: %v", dir, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	if len(entries) != 0 {
		t.Fatalf("failed restore left staging files: %v", entries)
	}
}

func TestSetBackupDirValidates(t *testing.T) {
	v, _ := tmpVault(t)
	if _, err := v.Backup(); !errors.Is(err, ErrNoBackupDir) {
		t.Fatalf("backup without folder: %v", err)
	}
	inside := filepath.Join(v.dir, "backups")
	os.Mkdir(inside, 0700)
	newInside := filepath.Join(v.dir, "new")
	for _, dir := range []string{"relative/dir", v.dir, inside, newInside} {
		var verr *ValidationError
		if err := v.SetBackupDir(dir); !errors.As(err, &verr) {
			t.Errorf("SetBackupDir(%q) = %v, want a validation error", dir, err)
		}
	}
	if _, err := os.Stat(newInside); !errors.Is(err, os.ErrNotExist) {
		t.Error("a refused folder was created")
	}
	// A new folder outside the vault is created, owner-only.
	missing := filepath.Join(t.TempDir(), "missing", "deeper")
	if err := v.SetBackupDir(missing); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(missing); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("created folder: %v, %v", info, err)
	}
}

// A restored vault has no service tokens (one revoked after the backup must
// not come back) and reports the backup it came from as its last one.
func TestRestoreDropsTokensAndKeepsBackupTime(t *testing.T) {
	v, sk, _ := backupVault(t)
	tok, err := v.CreateServiceToken("agent", "notes.*", 24*time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	path, err := v.Backup()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.RevokeServiceToken(tok); err != nil {
		t.Fatal(err)
	}
	last := v.backupStatus().Last
	target := filepath.Join(t.TempDir(), "restored")
	if _, err := RestoreBackup(path, target, testPassword, sk); err != nil {
		t.Fatal(err)
	}
	r, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, ok := r.ValidateServiceToken(tok); ok {
		t.Error("revoked token valid after restore")
	}
	if got := r.backupStatus().Last; got != last {
		t.Errorf("restored last backup = %q, want %q", got, last)
	}
}

// Work dirs an interrupted restore or check left behind are removed by the
// next one; a recent one (maybe still in use) is kept, and so is a vault
// whose name looks like one (pvault --vault restore-test keeps ~/.pvault-restore-test).
func TestRestoreRemovesStaleWorkDirs(t *testing.T) {
	_, sk, path := backedUpVault(t)
	parent := t.TempDir()
	stale := filepath.Join(parent, restoreWorkPrefix+"1")
	fresh := filepath.Join(parent, restoreWorkPrefix+"2")
	checkStale := filepath.Join(os.TempDir(), "pvault-check-test-stale")
	for _, d := range []string{stale, fresh, checkStale} {
		if err := os.MkdirAll(filepath.Join(d, "vault"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	defer os.RemoveAll(checkStale)
	named := filepath.Join(parent, ".pvault-restore-test")
	if _, err := Init(named, testPassword); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for _, d := range []string{stale, checkStale, named} {
		os.Chtimes(d, old, old)
	}

	if _, err := RestoreBackup(path, filepath.Join(parent, "restored"), testPassword, sk); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckBackup(path, testPassword, sk); err != nil {
		t.Fatal(err)
	}
	for d, want := range map[string]bool{stale: false, fresh: true, checkStale: false, filepath.Join(named, "secret.key"): true} {
		if _, err := os.Stat(d); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", d, err == nil, want)
		}
	}
}

// A copy restored next to the original (to look at an old backup) keeps the
// backup folder but writes its own series there, so its backups never prune
// the original's.
func TestRestoredCopyKeepsOriginalBackups(t *testing.T) {
	v, sk, dir := backupVault(t)
	for i := 0; i < backupKeep; i++ {
		if _, err := v.Backup(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond) // file names are to the millisecond
	}
	originals := backupFiles(t, dir)
	if len(originals) != backupKeep {
		t.Fatalf("%d backups, want %d", len(originals), backupKeep)
	}

	target := filepath.Join(t.TempDir(), "copy")
	if _, err := RestoreBackup(filepath.Join(dir, originals[0]), target, testPassword, sk); err != nil {
		t.Fatal(err)
	}
	r, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	if r.backupSeries() == v.backupSeries() {
		t.Fatal("restored copy continues the original's backup series")
	}
	for i := 0; i < 3; i++ {
		if err := r.Set("notes.motto", fmt.Sprintf("copy %d", i), ""); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Backup(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	for _, name := range originals {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("original's backup %s pruned by the restored copy: %v", name, err)
		}
	}
	if got := len(backupFiles(t, dir)); got != backupKeep+3 {
		t.Errorf("%d backups in the folder, want %d", got, backupKeep+3)
	}
}

func TestAutomaticBackupAfterChange(t *testing.T) {
	old := backupDelay
	backupDelay = 20 * time.Millisecond
	defer func() { backupDelay = old }()

	v, _, dir := backupVault(t)
	if err := v.Set("notes.motto", "carpe noctem", ""); err != nil {
		t.Fatal(err)
	}
	// The file is renamed into place before the status records it, so wait
	// for the status.
	deadline := time.Now().Add(5 * time.Second)
	for v.backupStatus().Last == "" {
		if time.Now().After(deadline) {
			t.Fatal("no automatic backup after a change")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := v.backupStatus(); !st.Configured || st.Error != "" {
		t.Fatalf("status after backup = %+v", st)
	}
	if len(backupFiles(t, dir)) == 0 {
		t.Fatal("status says backed up, but no backup file")
	}
}

func TestLockWritesPendingBackup(t *testing.T) {
	old := backupDelay
	backupDelay = time.Hour
	defer func() { backupDelay = old }()

	for _, auto := range []bool{false, true} {
		v, sk, dir := backupVault(t)
		if auto {
			v.mu.RLock()
			s := v.session
			v.mu.RUnlock()
			s.autoLock()
		} else {
			v.Lock()
		}
		if status, _ := v.Status(); !status.Locked {
			t.Fatal("vault still unlocked")
		}
		files := backupFiles(t, dir)
		if len(files) != 1 {
			t.Fatalf("auto=%v: %d backups after lock, want 1", auto, len(files))
		}
		if _, err := CheckBackup(filepath.Join(dir, files[0]), testPassword, sk); err != nil {
			t.Fatal(err)
		}
		// Nothing changed since: locking again writes nothing new.
		v.Unlock(testPassword, sk)
		v.Lock()
		if n := len(backupFiles(t, dir)); n != 1 {
			t.Fatalf("auto=%v: %d backups after an unchanged lock, want 1", auto, n)
		}
	}
}

func TestPruneBackups(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	// 15 backups ten minutes apart today, then two a day going back 45 days.
	var all []string
	for i := 0; i < 15; i++ {
		all = append(all, now.Add(-time.Duration(i)*10*time.Minute).Format(backupTimeFormat))
	}
	for d := 1; d <= 45; d++ {
		day := now.AddDate(0, 0, -d)
		all = append(all, day.Format(backupTimeFormat), day.Add(-time.Hour).Format(backupTimeFormat))
	}
	const id, otherID = "0123abcd", "ffff0000"
	for _, stamp := range all {
		os.WriteFile(filepath.Join(dir, "pvault-"+id+"-"+stamp+".pvbackup"), nil, 0600)
	}
	// Another vault's old backup, an unrelated file, and temp files left by
	// a crash (one stale, one possibly in progress).
	others := []string{"pvault-" + otherID + "-" + now.AddDate(0, 0, -40).Format(backupTimeFormat) + ".pvbackup", "unrelated.txt", ".pvault-backup-new.tmp"}
	for _, name := range append(others, ".pvault-backup-old.tmp") {
		os.WriteFile(filepath.Join(dir, name), nil, 0600)
	}
	old := now.Add(-48 * time.Hour)
	os.Chtimes(filepath.Join(dir, ".pvault-backup-old.tmp"), old, old)
	os.Chtimes(filepath.Join(dir, ".pvault-backup-new.tmp"), now, now)

	pruneBackups(dir, id, now)

	for _, name := range others {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("pruning removed %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".pvault-backup-old.tmp")); err == nil {
		t.Error("stale temp file was kept")
	}

	var kept []string
	for _, name := range backupFiles(t, dir) {
		if strings.Contains(name, id) {
			kept = append(kept, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(kept)))
	// 10 newest (all today), then the newest of each earlier day within 30 days.
	if len(kept) != 10+29 {
		t.Fatalf("kept %d backups, want 39: %v", len(kept), kept)
	}
	for i, name := range kept[10:] {
		want := "pvault-" + id + "-" + now.AddDate(0, 0, -(i+1)).Format(backupTimeFormat) + ".pvbackup"
		if name != want {
			t.Fatalf("kept[%d] = %s, want %s", 10+i, name, want)
		}
	}
}

func TestBackupFolderInsideVaultIsDetected(t *testing.T) {
	v, _ := tmpVault(t)
	inside := filepath.Join(v.dir, "documents")
	os.MkdirAll(inside, 0700)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(v.dir, link); err != nil {
		t.Fatal(err)
	}
	upper := filepath.Join(filepath.Dir(v.dir), strings.ToUpper(filepath.Base(v.dir)), "documents")
	// A link elsewhere that points into the vault.
	intoDocs := filepath.Join(t.TempDir(), "backups")
	if err := os.Symlink(inside, intoDocs); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{inside, link, filepath.Join(link, "documents"), upper, intoDocs} {
		if _, err := os.Stat(dir); err != nil {
			continue // case-sensitive disk: upper doesn't exist
		}
		if !isWithin(dir, v.dir) {
			t.Errorf("isWithin(%q) = false", dir)
		}
	}
	// A relative vault dir is compared as files too.
	wd, _ := os.Getwd()
	rel, err := filepath.Rel(wd, v.dir)
	if err == nil && !isWithin(inside, rel) {
		t.Errorf("isWithin with relative base %q = false", rel)
	}
	if isWithin(t.TempDir(), v.dir) {
		t.Error("unrelated folder reported inside the vault")
	}
}

// The public status says what went wrong without naming the folder, and
// says the folder is missing only when it is.
func TestBackupStatusError(t *testing.T) {
	v, _, dir := backupVault(t)
	os.RemoveAll(dir)
	if _, err := v.Backup(); err == nil {
		t.Fatal("backup to a removed folder succeeded")
	}
	st := v.backupStatus()
	if st.Error == "" || strings.Contains(st.Error, dir) {
		t.Fatalf("status error = %q", st.Error)
	}

	dir = t.TempDir()
	err := &os.PathError{Op: "open", Path: "/vault/documents/x.enc", Err: os.ErrNotExist}
	if msg := backupStatusError(err, dir); strings.Contains(msg, "folder is missing") {
		t.Fatalf("existing folder reported missing: %q", msg)
	}
	if msg := backupStatusError(err, filepath.Join(dir, "gone")); !strings.Contains(msg, "folder is missing") {
		t.Fatalf("missing folder not reported: %q", msg)
	}
}

func TestBackupLeavesOutDocumentWithoutFile(t *testing.T) {
	v, sk, _ := backupVault(t)
	docs, _ := v.ListDocuments(nil)
	os.Remove(v.documentPath(docs[0].ID))
	path, err := v.Backup()
	if err != nil {
		t.Fatalf("backup with a lost document file: %v", err)
	}
	info, err := CheckBackup(path, testPassword, sk)
	if err != nil {
		t.Fatal(err)
	}
	if info.Documents != 0 || info.Fields != 2 {
		t.Fatalf("check = %+v, want the fields and no documents", info)
	}
}

func TestBackupRemovesStaleWorkDir(t *testing.T) {
	v, _, _ := backupVault(t)
	stale := filepath.Join(v.dir, ".backup-123456")
	if err := os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "vault.db"), []byte("snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Backup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale work dir still there after a backup (stat: %v)", err)
	}
}
