package filesync

import (
	"errors"
	"strings"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/wincred"
)

// memItems is a store's entries in memory, failing a save when told to.
type memItems struct {
	m        map[string]string
	failSave string // a name whose save fails
}

func (memItems) where() string                       { return "test store" }
func (m memItems) loadItem(s string) (string, error) { return m.m[s], nil }
func (m memItems) deleteItem(s string) error         { delete(m.m, s); return nil }
func (m memItems) saveItem(s, v string) error {
	if m.failSave != "" && strings.HasPrefix(s, m.failSave) {
		return errors.New("disk full")
	}
	m.m[s] = v
	return nil
}

// A long value is split, read back whole, replaced by a new generation that
// leaves nothing of the old one, and deleted entirely.
func TestChunkedStore(t *testing.T) {
	it := memItems{m: map[string]string{}}
	long := strings.Repeat(`{"refresh_token":"0.AXsA-abc_DEF.ghi"}`, 120)
	if err := saveChunked(it, "pvault-files-x", long); err != nil {
		t.Fatal(err)
	}
	if len(it.m) < 3 {
		t.Fatalf("%d entries for a long value", len(it.m))
	}
	if got, err := loadChunked(it, "pvault-files-x"); err != nil || got != long {
		t.Fatalf("read back %d bytes, %v", len(got), err)
	}
	if err := saveChunked(it, "pvault-files-x", "short"); err != nil {
		t.Fatal(err)
	}
	if len(it.m) != 2 {
		t.Fatalf("old generation left: %v", it.m)
	}
	// A failed save keeps the old value whole.
	it.failSave = "pvault-files-x.g"
	if err := saveChunked(it, "pvault-files-x", long); err == nil {
		t.Fatal("save didn't fail")
	}
	if got, _ := loadChunked(it, "pvault-files-x"); got != "short" {
		t.Fatalf("after a failed save: %q", got)
	}
	it.failSave = ""
	if err := deleteChunked(it, "pvault-files-x"); err != nil || len(it.m) != 0 {
		t.Fatalf("after delete: %v %v", it.m, err)
	}
	if got, err := loadChunked(it, "pvault-files-x"); got != "" || err != nil {
		t.Fatalf("deleted: %q %v", got, err)
	}
}

// Every entry fits in one Credential Manager credential (stored as UTF-16).
func TestChunksFitCredentialManager(t *testing.T) {
	if 2*chunkSize > wincred.MaxSecretBytes {
		t.Fatalf("a %d-character chunk is %d bytes as UTF-16, over %d", chunkSize, 2*chunkSize, wincred.MaxSecretBytes)
	}
}

func TestSystemStoreOutsideWindows(t *testing.T) {
	if _, ok := SystemStore().(KeychainStore); !ok {
		t.Skip("Windows uses Credential Manager")
	}
	if _, err := (CredentialStore{}).Load("pvault-files-x"); err == nil {
		t.Error("Credential Manager used outside Windows")
	}
}
