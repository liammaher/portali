package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDocument_RoundTripAndEncryptedAtRest(t *testing.T) {
	v, _ := tmpVault(t)
	content := []byte("%PDF-1.7 permesso di soggiorno scan for MARIO ROSSI")

	doc, err := v.AddDocument("permesso_soggiorno", "/Users/x/Desktop/permesso Mario.pdf", "application/pdf", content, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "permesso Mario.pdf" || doc.Sensitivity != "sensitive" || doc.Size != int64(len(content)) {
		t.Fatalf("unexpected metadata: %+v", doc)
	}

	onDisk, err := os.ReadFile(v.documentPath(doc.ID))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(onDisk, []byte("MARIO")) {
		t.Fatal("document stored in plaintext")
	}
	if st, _ := os.Stat(v.documentPath(doc.ID)); st.Mode().Perm() != 0600 {
		t.Fatalf("document file mode = %v, want 0600", st.Mode().Perm())
	}
	raw, _ := v.db.GetDocument(doc.ID)
	if strings.Contains(raw.Name, "Mario") {
		t.Fatal("filename stored in plaintext")
	}

	info, data, err := v.GetDocument(doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, content) || info.Name != "permesso Mario.pdf" {
		t.Fatalf("round trip mismatch: %q %q", info.Name, data)
	}
}

func TestDocument_SwappedCiphertextRejected(t *testing.T) {
	v, _ := tmpVault(t)
	a, _ := v.AddDocument("passaporto", "a.pdf", "application/pdf", []byte("passport A"), "")
	b, _ := v.AddDocument("passaporto", "b.pdf", "application/pdf", []byte("passport B"), "")

	// Copy A's ciphertext over B's file: same key, but bound to A's ID.
	cipherA, _ := os.ReadFile(v.documentPath(a.ID))
	os.WriteFile(v.documentPath(b.ID), cipherA, 0600)
	if _, _, err := v.GetDocument(b.ID); err == nil {
		t.Fatal("swapped ciphertext should fail to decrypt")
	}
}

func TestDocument_InvalidIDsAndInputs(t *testing.T) {
	v, _ := tmpVault(t)
	for _, id := range []string{"../vault", "../../etc/passwd", "", "ABC", strings.Repeat("g", 32)} {
		if info, _, err := v.GetDocument(id); info != nil || err != nil {
			t.Errorf("GetDocument(%q) = %v, %v; want nil, nil", id, info, err)
		}
		if ok, err := v.DeleteDocument(id); ok || err != nil {
			t.Errorf("DeleteDocument(%q) = %v, %v", id, ok, err)
		}
	}

	var verr *ValidationError
	cases := []struct {
		category, name, ct string
		data               []byte
	}{
		{"bad/cat", "a.pdf", "application/pdf", []byte("x")},
		{"passaporto", "", "application/pdf", []byte("x")},
		{"passaporto", "a\x00b.pdf", "application/pdf", []byte("x")},
		{"passaporto", "a.pdf", "not a type", []byte("x")},
		{"passaporto", "a.pdf", "application/pdf", nil},
		{"passaporto", "a.pdf", "application/pdf", make([]byte, MaxDocumentSize+1)},
	}
	for _, c := range cases {
		if _, err := v.AddDocument(c.category, c.name, c.ct, c.data, ""); !errors.As(err, &verr) {
			t.Errorf("AddDocument(%q, %q, %q, %d bytes) = %v, want ValidationError", c.category, c.name, c.ct, len(c.data), err)
		}
	}
	if _, err := v.AddDocument("spid", "creds.png", "image/png", []byte("x"), ""); !errors.Is(err, ErrForbiddenField) {
		t.Errorf("spid document: got %v, want ErrForbiddenField", err)
	}
	if _, err := v.AddDocument("passaporto", "a.pdf", "application/pdf", []byte("x"), "top"); err != ErrInvalidTier {
		t.Errorf("bad tier: got %v", err)
	}
}

func TestDocument_DeleteListAndStatus(t *testing.T) {
	v, _ := tmpVault(t)
	a, _ := v.AddDocument("passaporto", "p.pdf", "application/pdf", []byte("p"), "")
	v.AddDocument("tessera_sanitaria", "ts.jpg", "image/jpeg", []byte("t"), "standard")
	v.AddDocument("patente", "pat.jpg", "image/jpeg", []byte("l"), "critical")

	all, _ := v.ListDocuments(nil)
	if len(all) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(all))
	}
	capped, _ := v.ListDocuments(func(id, tier string) bool { return tier != "critical" && id != "tessera_sanitaria.documents" })
	if len(capped) != 1 || capped[0].Category != "passaporto" {
		t.Fatalf("filter not applied: %+v", capped)
	}
	if st, _ := v.Status(); st.Documents != 3 {
		t.Fatalf("status documents = %d", st.Documents)
	}

	if ok, err := v.DeleteDocument(a.ID); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if _, err := os.Stat(v.documentPath(a.ID)); !os.IsNotExist(err) {
		t.Fatal("encrypted file not removed")
	}
	if info, _, _ := v.GetDocument(a.ID); info != nil {
		t.Fatal("deleted document still readable")
	}
	leftovers, _ := filepath.Glob(filepath.Join(v.dir, "documents", ".upload-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func TestDocument_RequiresUnlocked(t *testing.T) {
	v, _ := tmpVault(t)
	v.Lock()
	if _, err := v.AddDocument("passaporto", "p.pdf", "application/pdf", []byte("p"), ""); err != ErrLocked {
		t.Fatalf("add while locked: %v", err)
	}
	if _, err := v.ListDocuments(nil); err != ErrLocked {
		t.Fatalf("list while locked: %v", err)
	}
}

func TestDates(t *testing.T) {
	for _, name := range []string{"data_scadenza", "membro1_data_nascita", "date_of_birth", "data_rilascio", "Data_Inizio"} {
		if !IsDateField(name) {
			t.Errorf("IsDateField(%q) = false", name)
		}
	}
	for _, name := range []string{"metadata", "numero", "update_note", "card_expiry", "codice_fiscale"} {
		if IsDateField(name) {
			t.Errorf("IsDateField(%q) = true", name)
		}
	}
	ok := map[string]string{
		"2027-03-15": "2027-03-15", "15/03/2027": "2027-03-15", "15-03-2027": "2027-03-15",
		"15.03.2027": "2027-03-15", "5/3/2027": "2027-03-05", " 2028-02-29 ": "2028-02-29",
	}
	for in, want := range ok {
		if got, err := NormalizeDate(in); err != nil || got != want {
			t.Errorf("NormalizeDate(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"31/02/2027", "2027-02-29", "2027/03/15", "March 15 2027", "15/13/2027", ""} {
		if _, err := NormalizeDate(in); err == nil {
			t.Errorf("NormalizeDate(%q) should fail", in)
		}
	}
	if got, err := NormalizeAndValidate("cie.data_scadenza", "01/08/2031"); err != nil || got != "2031-08-01" {
		t.Errorf("cie.data_scadenza normalize: %q %v", got, err)
	}
	if _, err := NormalizeAndValidate("passaporto.data_scadenza", "next year"); err == nil {
		t.Error("non-date in date field should be rejected")
	}
}

func TestExpiring(t *testing.T) {
	v, _ := tmpVault(t)
	now := time.Date(2026, 10, 24, 15, 0, 0, 0, time.UTC)
	v.Set("permesso_soggiorno.data_scadenza", "2026-12-01", "") // 38 days
	v.Set("passaporto.data_scadenza", "2026-10-01", "")         // expired 23 days ago
	v.Set("patente.data_scadenza", "2030-01-01", "")            // far future
	v.Set("cie.data_scadenza", "24/10/2026", "")                // today
	v.Set("tessera_sanitaria.data_scadenza", "2027-01-10", "critical")
	v.Set("passaporto.data_rilascio", "2016-10-01", "") // not an expiry field

	items, err := v.Expiring(90*24*time.Hour, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Document)
	}
	want := "passaporto,cie,permesso_soggiorno,tessera_sanitaria"
	if strings.Join(got, ",") != want {
		t.Fatalf("expiring order = %v, want %s", got, want)
	}
	if !items[0].Expired || items[0].DaysLeft != -23 || items[1].DaysLeft != 0 || items[2].DaysLeft != 38 {
		t.Fatalf("days wrong: %+v", items)
	}

	capped, _ := v.Expiring(90*24*time.Hour, func(id, tier string) bool { return tier != "critical" }, now)
	if len(capped) != 3 {
		t.Fatalf("filter ignored: %+v", capped)
	}
}
