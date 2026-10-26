package vault

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// setDocumentsCreatedAt dates every document at at. Times are stored to the
// second, and a scan from the second its pratica closed counts as after it,
// so tests that upload and close at once date the scans back first.
func setDocumentsCreatedAt(t *testing.T, v *Vault, at time.Time) {
	t.Helper()
	execSQL(t, v, "UPDATE vault_documents SET created_at = ?", at.UTC().Format(time.RFC3339))
}

// setClosedAt dates when a closed pratica's stato closed.
func setClosedAt(t *testing.T, v *Vault, id string, at time.Time) {
	t.Helper()
	execSQL(t, v, "UPDATE vault_fields SET closed_at = ? WHERE id = ?", at.UTC().Format(time.RFC3339), id)
}

// execSQL runs a statement on the vault's database, for states only an
// older version could leave.
func execSQL(t *testing.T, v *Vault, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(v.dir, "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestProcura_PersonOnly(t *testing.T) {
	for _, kind := range []string{KindPersona, KindImpresa} {
		v := tmpVaultKind(t, kind)
		rel := map[string][2]string{KindPersona: {"mamma:famiglia.relazione", "genitore"}, KindImpresa: {"rossi:ruolo.tipo", "cliente"}}[kind]
		mustSet(t, v, rel[0], rel[1])
		slug, _ := SplitPerson(rel[0])
		mustSet(t, v, slug+":procura.ente", "SUE Comune di Napoli")
		mustSet(t, v, slug+":procura.oggetto", "CILA via Roma 1")
		mustSet(t, v, slug+":procura2.ente", "Agenzia delle Entrate")
		mustSet(t, v, slug+":procura.data_conferimento", "2026-03-01")
		if err := v.Set(slug+":procura.data_scadenza", "not a date", ""); err == nil {
			t.Errorf("%s: a bad procura date was accepted", kind)
		}
		for _, id := range []string{"procura.ente", "procura2.ente", "procura-3.oggetto"} {
			var verr *ValidationError
			if err := v.Set(id, "x", ""); !errors.As(err, &verr) || !strings.Contains(err.Error(), "another person gave you") {
				t.Errorf("%s: %s without a person: %v", kind, id, err)
			}
		}
		if _, err := v.AddDocument("procura", "procura.pdf", "application/pdf", []byte("%PDF"), ""); err == nil {
			t.Errorf("%s: a procura document without a person was accepted", kind)
		}
		d, err := v.AddDocument(slug+":procura", "procura.pdf", "application/pdf", []byte("%PDF"), "")
		if err != nil || d.Sensitivity != DefaultDocumentSensitivity {
			t.Errorf("%s: procura document: %+v %v", kind, d, err)
		}
		if f, _ := v.Get(slug + ":procura.ente"); f.Sensitivity != "standard" {
			t.Errorf("%s: procura.ente tier %q", kind, f.Sensitivity)
		}
	}
}

func TestProcura_Expiring(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	mustSet(t, v, "rossi:ruolo.tipo", "cliente")
	mustSet(t, v, "rossi:procura.data_scadenza", "2026-11-15")
	items, err := v.Expiring(90*24*time.Hour, nil, time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Document != "procura" || items[0].Person != "rossi" || items[0].DaysLeft != 20 || items[0].Kind != "" {
		t.Fatalf("expiring = %+v", items)
	}
}

func TestRetention(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	mustSet(t, v, "rossi:ruolo.tipo", "cliente")
	mustSet(t, v, "rossi:ruolo.data_fine", "2016-03-01")
	mustSet(t, v, "verdi:ruolo.tipo", "cliente")
	mustSet(t, v, "verdi:ruolo.data_fine", "2016-12-01")
	mustSet(t, v, "bianchi:ruolo.tipo", "dipendente")
	mustSet(t, v, "bianchi:ruolo.data_fine", "2025-01-01")
	window := 90 * 24 * time.Hour

	// No default: nothing is reported until the user sets a period.
	if years, err := v.RetentionYears(); err != nil || years != 0 {
		t.Fatalf("default retention = %d, %v", years, err)
	}
	if items, _ := v.Expiring(window, nil, now); len(items) != 0 {
		t.Fatalf("retention reported without a setting: %+v", items)
	}

	for _, bad := range []int{-1, MaxRetentionYears + 1} {
		if err := v.SetRetentionYears(bad); err == nil {
			t.Errorf("SetRetentionYears(%d) accepted", bad)
		}
	}
	if err := v.SetRetentionYears(10); err != nil {
		t.Fatal(err)
	}
	items, err := v.Expiring(window, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ExpiryInfo{}
	for _, it := range items {
		if it.Kind != ExpiryRetention {
			t.Fatalf("unexpected entry %+v", it)
		}
		got[it.Person] = it
	}
	if len(got) != 2 || !got["rossi"].Expired || got["rossi"].ExpiresOn != "2026-03-01" ||
		got["verdi"].Expired || got["verdi"].ExpiresOn != "2026-12-01" || got["rossi"].FieldID != "rossi:ruolo.data_fine" {
		t.Fatalf("retention entries = %+v", items)
	}
	// Only from what the caller can read.
	onlyVerdi := func(id, _ string) bool { return strings.HasPrefix(id, "verdi:") }
	if items, _ := v.Expiring(window, onlyVerdi, now); len(items) != 1 || items[0].Person != "verdi" {
		t.Fatalf("filtered retention = %+v", items)
	}
	if err := v.SetRetentionYears(0); err != nil {
		t.Fatal(err)
	}
	if items, _ := v.Expiring(window, nil, now); len(items) != 0 {
		t.Fatalf("retention reported after unset: %+v", items)
	}
	if entries, _ := v.AuditLog(10); !hasAudit(entries, "retention_set") {
		t.Error("no retention_set audit entry")
	}
}

func TestRetention_ScansAfterPratiche(t *testing.T) {
	now := time.Now().UTC() // pratiche close now
	window := 90 * 24 * time.Hour
	v := tmpVaultKind(t, KindImpresa)
	for _, slug := range []string{"rossi", "verdi", "neri"} {
		mustSet(t, v, slug+":ruolo.tipo", "cliente")
		for _, cat := range []string{"cie", "procura", "albo"} {
			if _, err := v.AddDocument(slug+":"+cat, cat+".pdf", "application/pdf", []byte("%PDF"), ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := v.AddDocument("rossi:cie", "retro.pdf", "application/pdf", []byte("%PDF"), ""); err != nil {
		t.Fatal(err)
	}
	setDocumentsCreatedAt(t, v, now.Add(-time.Hour))
	// rossi: both pratiche closed. verdi: one still open. neri: none.
	mustSet(t, v, "pratiche.cila-roma_persona", "rossi")
	mustSet(t, v, "pratiche.cila-roma_stato", "completata")
	mustSet(t, v, "pratiche.docfa-roma_persona", "rossi")
	mustSet(t, v, "pratiche.docfa-roma_stato", "annullata")
	mustSet(t, v, "pratiche.scia-verdi_persona", "verdi")
	mustSet(t, v, "pratiche.scia-verdi_stato", "completata")
	mustSet(t, v, "pratiche.suap-verdi_persona", "verdi")
	mustSet(t, v, "pratiche.suap-verdi_stato", "in_attesa")

	// Nothing until the user sets a period for scans (D8: none is verified).
	if items, err := v.Expiring(window, nil, now); err != nil || len(items) != 0 {
		t.Fatalf("no period set: %+v, %v", items, err)
	}
	if err := v.SetScanDays(0); err != nil {
		t.Fatal(err)
	}
	items, err := v.Expiring(window, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		if it.Kind != ExpiryPraticheClosed || it.ExpiresOn != now.Format(dateLayout) {
			t.Fatalf("unexpected entry %+v", it)
		}
		got = append(got, it.FieldID)
	}
	if strings.Join(got, " ") != "rossi:cie.documents rossi:procura.documents" {
		t.Fatalf("scans due = %v", got)
	}
	// Only documents the caller can see.
	noProcura := func(id, _ string) bool { return !strings.Contains(id, "procura") }
	if items, _ := v.Expiring(window, noProcura, now); len(items) != 1 || items[0].Document != "cie" {
		t.Fatalf("filtered scans = %+v", items)
	}

	// Not when the caller can't see every pratica naming the client: a
	// hidden one may be open.
	hideDocfa := func(id, _ string) bool { return !strings.HasPrefix(id, "pratiche.docfa-roma_") }
	if items, _ := v.Expiring(window, hideDocfa, now); len(items) != 0 {
		t.Fatalf("partial view of rossi's pratiche: %+v", items)
	}
	// Nor without their ruolo.
	hideRuolo := func(id, _ string) bool { return !strings.HasSuffix(id, ":ruolo.tipo") }
	if items, _ := v.Expiring(window, hideRuolo, now); len(items) != 0 {
		t.Fatalf("without ruolo.tipo: %+v", items)
	}
	// Only clients: the titolare's ID is used by the business's own pratiche
	// (persona io), which don't name them.
	mustSet(t, v, "bianchi:ruolo.tipo", "titolare")
	if _, err := v.AddDocument("bianchi:cie", "cie.pdf", "application/pdf", []byte("%PDF"), ""); err != nil {
		t.Fatal(err)
	}
	setDocumentsCreatedAt(t, v, now.Add(-time.Hour))
	mustSet(t, v, "pratiche.albo-bianchi_persona", "bianchi")
	mustSet(t, v, "pratiche.albo-bianchi_stato", "completata")
	mustSet(t, v, "pratiche.scia-bar_stato", "in_attesa")
	if items, _ := v.Expiring(window, nil, now); len(items) != 2 {
		t.Fatalf("titolare's scans flagged: %+v", items)
	}
	// A client who is also a socio stays a client.
	mustSet(t, v, "bianchi:ruolo.tipo", "socio,cliente")
	if items, _ := v.Expiring(window, nil, now); len(items) != 3 || items[0].Person != "bianchi" {
		t.Fatalf("socio and cliente: %+v", items)
	}

	// A personal vault keeps family scans: no such entries.
	p := tmpVaultKind(t, KindPersona)
	if err := p.SetScanDays(0); err != nil {
		t.Fatal(err)
	}
	mustSet(t, p, "leo:famiglia.relazione", "figlio")
	p.AddDocument("leo:cie", "cie.pdf", "application/pdf", []byte("%PDF"), "")
	mustSet(t, p, "pratiche.cie-leo_persona", "leo")
	mustSet(t, p, "pratiche.cie-leo_stato", "completata")
	if items, _ := p.Expiring(window, nil, now); len(items) != 0 {
		t.Fatalf("personal vault: %+v", items)
	}
}

func TestExportPerson(t *testing.T) {
	v := tmpVaultKind(t, KindPersona)
	mustSet(t, v, "leo:famiglia.relazione", "figlio")
	mustSet(t, v, "leo:cie.numero", "CA12345AB")
	mustSet(t, v, "leo:spid.password", "hunter2")
	mustSet(t, v, "giulia:famiglia.relazione", "coniuge")
	mustSet(t, v, "identity.first_name", "Liam")
	if _, err := v.AddDocument("leo:cie", "fronte.jpg", "image/jpeg", []byte("JPEG"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := v.AddDocument("giulia:cie", "g.jpg", "image/jpeg", []byte("G"), ""); err != nil {
		t.Fatal(err)
	}

	out, err := v.ExportPerson("leo", false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range out.Fields {
		ids = append(ids, f.ID+"="+f.Value)
	}
	if strings.Join(ids, " ") != "leo:cie.numero=CA12345AB leo:famiglia.relazione=figlio" || out.Omitted != 1 {
		t.Fatalf("fields = %v, omitted %d", ids, out.Omitted)
	}
	if len(out.Documents) != 1 || out.Documents[0].Name != "fronte.jpg" || string(out.Documents[0].Data) != "JPEG" {
		t.Fatalf("documents = %+v", out.Documents)
	}
	out, err = v.ExportPerson("leo", true)
	if err != nil || len(out.Fields) != 3 || out.Omitted != 0 {
		t.Fatalf("include personal: %+v %v", out, err)
	}
	// A lost file is reported, not fatal.
	lost, err := v.AddDocument("leo:passaporto", "pass.pdf", "application/pdf", []byte("P"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(v.documentPath(lost.ID)); err != nil {
		t.Fatal(err)
	}
	out, err = v.ExportPerson("leo", false)
	if err != nil || len(out.Documents) != 1 || len(out.Missing) != 1 || out.Missing[0].Name != "pass.pdf" {
		t.Fatalf("export with a lost file: %+v %v", out, err)
	}
	if _, err := v.ExportPerson("marco", false); !errors.Is(err, ErrPersonNotFound) {
		t.Fatalf("missing person: %v", err)
	}
	if _, err := v.ExportPerson("io", false); err == nil {
		t.Fatal("exported io")
	}
	entries, _ := v.AuditLog(10)
	if !hasAudit(entries, "member_export") {
		t.Error("no member_export audit entry")
	}
	v.Lock()
	if _, err := v.ExportPerson("leo", false); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked export: %v", err)
	}
}

func hasAudit(entries []store.AuditEntry, action string) bool {
	for _, e := range entries {
		if e.Action == action {
			return true
		}
	}
	return false
}

// A scan handed over after the client's pratiche closed (for a new job, or
// for a later person given a removed person's slug) is not due, and neither is
// a procura still in force. Two spellings of ruolo always give one answer.
func TestRetention_ScansAfterPratiche_DatesAndProcura(t *testing.T) {
	now := time.Now().UTC()
	window := 90 * 24 * time.Hour
	v := tmpVaultKind(t, KindImpresa)
	if err := v.SetScanDays(0); err != nil {
		t.Fatal(err)
	}
	mustSet(t, v, "rossi:ruolo.tipo", "cliente")
	for _, cat := range []string{"cie", "procura"} {
		if _, err := v.AddDocument("rossi:"+cat, cat+".pdf", "application/pdf", []byte("%PDF"), ""); err != nil {
			t.Fatal(err)
		}
	}
	setDocumentsCreatedAt(t, v, now.Add(-time.Minute))
	mustSet(t, v, "pratiche.cila-roma_persona", "rossi")
	mustSet(t, v, "pratiche.cila-roma_stato", "completata")
	due := func() string {
		t.Helper()
		items, err := v.Expiring(window, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, it := range items {
			if it.Kind == ExpiryPraticheClosed {
				got = append(got, it.FieldID)
			}
		}
		return strings.Join(got, " ")
	}
	for range 20 {
		if got := due(); got != "rossi:cie.documents rossi:procura.documents" {
			t.Fatalf("scans due = %q", got)
		}
	}

	mustSet(t, v, "rossi:procura.data_scadenza", now.AddDate(2, 0, 0).Format(dateLayout))
	if got := due(); got != "rossi:cie.documents" {
		t.Fatalf("procura in force: scans due = %q", got)
	}
	mustSet(t, v, "rossi:procura.data_scadenza", now.AddDate(-1, 0, 0).Format(dateLayout))
	if got := due(); got != "rossi:cie.documents rossi:procura.documents" {
		t.Fatalf("procura expired: scans due = %q", got)
	}

	// The pratica closed an hour ago; the scans arrived a minute ago.
	setClosedAt(t, v, "pratiche.cila-roma_stato", time.Now().Add(-time.Hour))
	if got := due(); got != "" {
		t.Fatalf("scans uploaded after the close: due = %q", got)
	}
	// Saving the same stato again, changing its tier, or correcting it to
	// another closing stato isn't a new close.
	mustSet(t, v, "pratiche.cila-roma_stato", "completata")
	if err := v.SetSensitivity("pratiche.cila-roma_stato", "sensitive"); err != nil {
		t.Fatal(err)
	}
	mustSet(t, v, "pratiche.cila-roma_stato", "annullata")
	if got := due(); got != "" {
		t.Fatalf("after re-saving or correcting the stato: due = %q", got)
	}
	// Reopening and closing it again is.
	mustSet(t, v, "pratiche.cila-roma_stato", "in_attesa")
	mustSet(t, v, "pratiche.cila-roma_stato", "completata")
	if got := due(); got != "rossi:cie.documents rossi:procura.documents" {
		t.Fatalf("closed again: due = %q", got)
	}

	// A scan from the very second the pratica closed may have come after it
	// (times are to the second), so it isn't due.
	doc, err := v.AddDocument("rossi:passaporto", "passaporto.pdf", "application/pdf", []byte("%PDF"), "")
	if err != nil {
		t.Fatal(err)
	}
	setClosedAt(t, v, "pratiche.cila-roma_stato", doc.CreatedAt)
	if got := due(); got != "rossi:cie.documents rossi:procura.documents" {
		t.Fatalf("scan from the second of the close: due = %q", got)
	}
}

// The period for scans is counted from the day the last pratica closed, and
// an entry shows once that day plus the period is within the window.
func TestRetention_ScanDays(t *testing.T) {
	v := tmpVaultKind(t, KindImpresa)
	for _, bad := range []int{MaxScanDays + 1, -2} {
		if err := v.SetScanDays(bad); err == nil {
			t.Errorf("SetScanDays(%d) accepted", bad)
		}
	}
	if _, set, _ := v.ScanDays(); set {
		t.Fatal("scan days set by default")
	}
	mustSet(t, v, "rossi:ruolo.tipo", "cliente")
	if _, err := v.AddDocument("rossi:cie", "cie.pdf", "application/pdf", []byte("%PDF"), ""); err != nil {
		t.Fatal(err)
	}
	setDocumentsCreatedAt(t, v, time.Now().Add(-time.Minute))
	mustSet(t, v, "pratiche.cila-roma_persona", "rossi")
	mustSet(t, v, "pratiche.cila-roma_stato", "completata")
	closedOn := time.Now().UTC()
	due := func(days int, within time.Duration) []ExpiryInfo {
		t.Helper()
		if err := v.SetScanDays(days); err != nil {
			t.Fatal(err)
		}
		items, err := v.Expiring(within, nil, closedOn)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	items := due(30, 90*24*time.Hour)
	if len(items) != 1 || items[0].ExpiresOn != closedOn.AddDate(0, 0, 30).Format(dateLayout) || items[0].Expired {
		t.Fatalf("30 days, 90-day window: %+v", items)
	}
	if items := due(30, 10*24*time.Hour); len(items) != 0 {
		t.Fatalf("30 days, 10-day window: %+v", items)
	}
	if days, set, _ := v.ScanDays(); !set || days != 30 {
		t.Fatalf("ScanDays() = %d, %v", days, set)
	}
	if err := v.SetScanDays(-1); err != nil {
		t.Fatal(err)
	}
	if _, set, _ := v.ScanDays(); set {
		t.Fatal("scan days still set after -1")
	}
	if items, _ := v.Expiring(90*24*time.Hour, nil, closedOn); len(items) != 0 {
		t.Fatalf("unset again: %+v", items)
	}
	entries, _ := v.AuditLog(50)
	if !hasAudit(entries, "scan_days_set") {
		t.Error("no scan_days_set audit entry")
	}
}

func TestRetentionEnd(t *testing.T) {
	for _, c := range []struct {
		from  string
		years int
		want  string
	}{
		{"2024-02-29", 1, "2025-02-28"}, // no 29 February: the month's last day
		{"2024-02-29", 4, "2028-02-29"},
		{"2024-03-31", 10, "2034-03-31"},
		{"2023-02-28", 1, "2024-02-28"},
	} {
		if end, ok := retentionEnd(c.from, c.years); !ok || end.Format(dateLayout) != c.want {
			t.Errorf("retentionEnd(%s, %d) = %s, %v; want %s", c.from, c.years, end.Format(dateLayout), ok, c.want)
		}
	}
}
