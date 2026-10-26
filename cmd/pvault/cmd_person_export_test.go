package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func TestExportFileName(t *testing.T) {
	used := map[string]bool{}
	for _, c := range []struct{ name, want string }{
		{"cie.pdf", "cie.pdf"},
		{"CIE.pdf", "CIE (2).pdf"},
		{"../../etc/passwd", "passwd"},
		{"...", "abc"},
		{"", "abc (2)"},
	} {
		if got := exportFileName(c.name, "abc", used); got != c.want {
			t.Errorf("exportFileName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWritePersonExport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rossi-export")
	out := &vault.PersonExport{
		Person: "rossi",
		Fields: []vault.FieldInfo{{ID: "rossi:procura.ente", Value: "SUE"}},
		Documents: []vault.ExportedDocument{
			{DocumentInfo: vault.DocumentInfo{ID: "a", Category: "rossi:procura", Name: "procura.pdf"}, Data: []byte("%PDF")},
			{DocumentInfo: vault.DocumentInfo{ID: "b", Category: "rossi:procura", Name: "procura.pdf"}, Data: []byte("%PDF2")},
		},
	}
	if err := writePersonExport(dir, out); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"documents/procura/procura.pdf":     "%PDF",
		"documents/procura/procura (2).pdf": "%PDF2",
	} {
		data, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || string(data) != want {
			t.Errorf("%s = %q, %v", path, data, err)
		}
		if info, _ := os.Stat(filepath.Join(dir, path)); info.Mode().Perm() != 0600 {
			t.Errorf("%s mode %v", path, info.Mode())
		}
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0700 {
		t.Errorf("dir mode %v", info.Mode())
	}
	var index struct {
		Person    string
		Fields    []vault.FieldInfo
		Documents []map[string]any
	}
	data, _ := os.ReadFile(filepath.Join(dir, "fields.json"))
	if err := json.Unmarshal(data, &index); err != nil || index.Person != "rossi" || len(index.Fields) != 1 ||
		len(index.Documents) != 2 || index.Documents[1]["file"] != "documents/procura/procura (2).pdf" || index.Documents[0]["data"] != nil {
		t.Fatalf("fields.json = %s (%v)", data, err)
	}
	// Never into an existing folder.
	if err := writePersonExport(dir, out); err == nil {
		t.Fatal("wrote into an existing folder")
	}
}
