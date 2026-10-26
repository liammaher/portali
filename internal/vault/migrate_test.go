package vault

import (
	"strings"
	"testing"
	"time"
)

// Unlocking makes legacy upper-case field IDs lower-case, re-encrypted under
// the lower-case category: the spelling written last wins, at the highest
// tier any spelling had.
func TestMigrate_LowerCaseFieldIDs(t *testing.T) {
	v, _ := tmpVault(t)
	hourAgo := time.Now().Add(-time.Hour)
	setLegacy(t, v, "Pratiche.cila-roma_stato", "in_attesa") // the only spelling
	mustSet(t, v, "identity.first_name", "Anna")
	if err := v.SetSensitivity("identity.first_name", "critical"); err != nil {
		t.Fatal(err)
	}
	execSQL(t, v, "UPDATE vault_fields SET updated_at = ? WHERE id = 'identity.first_name'", hourAgo.UTC().Format(time.RFC3339))
	setLegacy(t, v, "Identity.First_Name", "Annamaria") // newer: wins, keeps critical
	mustSet(t, v, "residenza.comune", "Napoli")
	setLegacyAt(t, v, "Residenza.comune", "Roma", hourAgo) // older: dropped
	relock(t, v)

	fields, err := v.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		if f.ID != strings.ToLower(f.ID) {
			t.Errorf("%s still has upper-case letters", f.ID)
		}
	}
	for id, want := range map[string]string{
		"pratiche.cila-roma_stato": "in_attesa",
		"identity.first_name":      "Annamaria",
		"residenza.comune":         "Napoli",
	} {
		if f, err := v.Get(id); err != nil || f.Value != want {
			t.Errorf("%s = %+v, %v; want %q", id, f, err, want)
		}
	}
	if f, _ := v.FieldMeta("identity.first_name"); f == nil || f.Sensitivity != "critical" {
		t.Errorf("identity.first_name tier = %+v, want critical kept", f)
	}
	entries, _ := v.AuditLog(50)
	if !hasAudit(entries, "case_migrate") {
		t.Error("no case_migrate audit entry")
	}
}

// A closed stato written before closing times were stored is dated at its
// last write when the vault unlocks; an open one stays undated.
func TestMigrate_DatesClosedPratiche(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "pratiche.tari-2026_stato", "completata")
	mustSet(t, v, "pratiche.cie-2026_stato", "in_attesa")
	written := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	execSQL(t, v, "UPDATE vault_fields SET closed_at = NULL, updated_at = ?", written.Format(time.RFC3339))
	relock(t, v)
	if f, _ := v.FieldMeta("pratiche.tari-2026_stato"); f == nil || !f.ClosedAt.Equal(written) {
		t.Errorf("closed stato: ClosedAt = %v, want %v", f.ClosedAt, written)
	}
	if f, _ := v.FieldMeta("pratiche.cie-2026_stato"); f == nil || !f.ClosedAt.IsZero() {
		t.Errorf("open stato: ClosedAt = %v, want none", f.ClosedAt)
	}
}

// The closing time is kept while the stato stays closed, even when it is
// corrected to another closing stato, and starts again after a reopen.
func TestClosedAt(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "pratiche.tari-2026_stato", "inviata")
	if f, _ := v.FieldMeta("pratiche.tari-2026_stato"); !f.ClosedAt.IsZero() {
		t.Fatalf("open stato dated %v", f.ClosedAt)
	}
	mustSet(t, v, "pratiche.tari-2026_stato", "completata")
	past := time.Now().Add(-time.Hour)
	setClosedAt(t, v, "pratiche.tari-2026_stato", past)
	mustSet(t, v, "pratiche.tari-2026_stato", "annullata")
	if f, _ := v.FieldMeta("pratiche.tari-2026_stato"); f.ClosedAt.Unix() != past.Unix() {
		t.Fatalf("corrected stato: ClosedAt = %v, want %v", f.ClosedAt, past)
	}
	mustSet(t, v, "pratiche.tari-2026_stato", "in_attesa")
	if f, _ := v.FieldMeta("pratiche.tari-2026_stato"); !f.ClosedAt.IsZero() {
		t.Fatalf("reopened stato: ClosedAt = %v", f.ClosedAt)
	}
	mustSet(t, v, "pratiche.tari-2026_stato", "completata")
	if f, _ := v.FieldMeta("pratiche.tari-2026_stato"); time.Since(f.ClosedAt) > time.Minute {
		t.Fatalf("closed again: ClosedAt = %v, want now", f.ClosedAt)
	}
}

// An older version reopening a stato leaves its closing time behind; the
// next unlock clears it, so closing again is dated afresh.
func TestMigrate_ClearsStaleClosedAt(t *testing.T) {
	v, _ := tmpVault(t)
	mustSet(t, v, "pratiche.tari-2026_stato", "completata")
	old := time.Now().Add(-48 * time.Hour)
	setClosedAt(t, v, "pratiche.tari-2026_stato", old)
	setLegacy(t, v, "pratiche.tari-2026_stato", "in_attesa") // as an older pvault writes: closed_at untouched
	relock(t, v)
	if f, _ := v.FieldMeta("pratiche.tari-2026_stato"); !f.ClosedAt.IsZero() {
		t.Fatalf("reopened stato kept ClosedAt %v", f.ClosedAt)
	}
	mustSet(t, v, "pratiche.tari-2026_stato", "completata")
	if f, _ := v.FieldMeta("pratiche.tari-2026_stato"); time.Since(f.ClosedAt) > time.Minute {
		t.Fatalf("closed again: ClosedAt = %v, want now", f.ClosedAt)
	}
}

// A legacy field that can't be decrypted stays as it is and is reported;
// the others are still made lower-case.
func TestMigrate_SkipsUnreadableFields(t *testing.T) {
	v, _ := tmpVault(t)
	setLegacy(t, v, "Pratiche.cila-roma_stato", "completata")
	setLegacy(t, v, "Notes.X", "x")
	execSQL(t, v, "UPDATE vault_fields SET value = 'not-base64!' WHERE id = 'Notes.X'")
	relock(t, v)
	if f, err := v.Get("pratiche.cila-roma_stato"); err != nil || f.Value != "completata" {
		t.Fatalf("readable legacy field not migrated: %+v, %v", f, err)
	}
	if f, _ := v.FieldMeta("Notes.X"); f == nil {
		t.Fatal("unreadable legacy field was dropped")
	}
	entries, _ := v.AuditLog(50)
	found := false
	for _, e := range entries {
		if e.Action == "migrate_failed" && strings.Contains(e.Purpose, "Notes.X") {
			found = true
		}
	}
	if !found {
		t.Error("no migrate_failed entry naming Notes.X")
	}
}

// A public field keeps its tier when its spelling is made lower-case: public
// ranks like no tier at all, which must not win.
func TestMigrate_LowerCaseKeepsPublicTier(t *testing.T) {
	v, _ := tmpVault(t)
	setLegacy(t, v, "Notes.Motto", "carpe diem") // the only spelling
	mustSet(t, v, "notes.colore", "blu")
	setLegacyAt(t, v, "Notes.Colore", "rosso", time.Now().Add(-time.Hour)) // older
	execSQL(t, v, "UPDATE vault_fields SET sensitivity = 'public'")
	relock(t, v)

	for _, id := range []string{"notes.motto", "notes.colore"} {
		if f, _ := v.FieldMeta(id); f == nil || f.Sensitivity != "public" {
			t.Errorf("%s tier = %+v, want public", id, f)
		}
		mustSet(t, v, id, "changed") // a field with no tier refuses every write
	}
}
