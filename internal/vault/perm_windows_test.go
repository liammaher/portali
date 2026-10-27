package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A private folder lets only this user in, and doesn't inherit anything.
func TestRestrictDir_OnlyThisUser(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RestrictDir(dir); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Error("the DACL still inherits from the parent")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("%d entries, want 1", dacl.AceCount)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)); !sid.Equals(me.User.Sid) {
		t.Errorf("the entry is for %s, not this user (%s)", sid, me.User.Sid)
	}
	// What is created inside inherits it.
	f := filepath.Join(dir, "secret.key")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fsd, err := windows.GetNamedSecurityInfo(f, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if fdacl, _, err := fsd.DACL(); err != nil || fdacl.AceCount != 1 {
		t.Errorf("file DACL: %v entries, %v", fdacl, err)
	}
}

// Making a folder private rewrites the access of what it holds, so a vault
// on Windows takes only a new or empty folder.
func TestInit_RefusesFolderWithFilesOnWindows(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir, "test-password"); err == nil || !strings.Contains(err.Error(), "new or empty folder") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vault.db")); err == nil {
		t.Fatal("a vault was made there anyway")
	}
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(empty, "test-password"); err != nil {
		t.Fatalf("empty folder: %v", err)
	}
}
