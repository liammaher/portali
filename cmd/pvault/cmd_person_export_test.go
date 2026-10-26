package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func TestWriteUniqueFile(t *testing.T) {
	folder := t.TempDir()
	for _, c := range []struct{ name, want string }{
		{"cie.pdf", "cie.pdf"},
		{"cie.pdf", "cie (2).pdf"},
		{"../../etc/passwd", "passwd"},
		{"...", "abc"},
		{"", "abc (2)"},
	} {
		got, err := writeUniqueFile(folder, c.name, "abc", []byte(c.name))
		if err != nil || got != c.want {
			t.Errorf("writeUniqueFile(%q) = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "passwd")); err != nil {
		t.Errorf("passwd not written inside the folder: %v", err)
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
			{DocumentInfo: vault.DocumentInfo{ID: "c", Category: "rossi:cie", Name: "procura.pdf"}, Data: []byte("CIE")},
		},
	}
	if err := writePersonExport(dir, out); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"documents/procura/procura.pdf":     "%PDF",
		"documents/procura/procura (2).pdf": "%PDF2",
		"documents/cie/procura.pdf":         "CIE", // names are unique per folder only
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
		len(index.Documents) != 3 || index.Documents[1]["file"] != "documents/procura/procura (2).pdf" || index.Documents[0]["data"] != nil {
		t.Fatalf("fields.json = %s (%v)", data, err)
	}
	// Never into an existing folder.
	if err := writePersonExport(dir, out); err == nil {
		t.Fatal("wrote into an existing folder")
	}
}
