package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// captureStdout runs f and returns what it printed to stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = old }()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

// runCmd runs a pvault command in-process, as `pvault args...`.
func runCmd(t *testing.T, f func(), args ...string) string {
	t.Helper()
	oldArgs := os.Args
	os.Args = append([]string{"pvault"}, args...)
	defer func() { os.Args = oldArgs }()
	cliToken = "" // each command asks for its own grant, as a new process would
	return captureStdout(t, f)
}

// TestClients_EndToEnd drives the phase 2 commands against a real business
// vault and API server: procura expiry, the retention setting and report,
// ID scans once a client's pratiche are closed and the period for them is
// set, and a person's export.
func TestClients_EndToEnd(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	soon := time.Now().AddDate(0, 0, 20).Format("2006-01-02")
	v, _ := touchIDVaultKind(t, vault.KindImpresa, map[string]string{
		"business.denominazione": "Studio Test",
	})
	verifiedKind = ""
	t.Cleanup(func() { verifiedKind = "" })
	for _, kv := range [][2]string{
		{"rossi:ruolo.tipo", "cliente"},
		{"rossi:identity.first_name", "Mario"},
		{"rossi:procura.ente", "SUE Comune di Napoli"},
		{"rossi:procura.data_scadenza", soon},
		{"rossi:ruolo.data_fine", "2015-01-10"},
		{"verdi:ruolo.tipo", "cliente"},
		{"pratiche.cila-rossi_persona", "rossi"},
		{"pratiche.cila-rossi_stato", "completata"},
		{"pratiche.scia-verdi_persona", "verdi"},
		{"pratiche.scia-verdi_stato", "in_attesa"},
	} {
		if err := v.Set(kv[0], kv[1], ""); err != nil {
			t.Fatalf("set %s: %v", kv[0], err)
		}
	}
	for _, d := range [][2]string{{"rossi:cie", "fronte.jpg"}, {"rossi:procura", "procura.pdf"}, {"verdi:cie", "cie.jpg"}} {
		if _, err := v.AddDocument(d[0], d[1], "application/octet-stream", []byte(d[0]), ""); err != nil {
			t.Fatal(err)
		}
	}

	// With Touch ID, 'set' gets a grant for that one field, which can still
	// name a client it can't otherwise see.
	runCmd(t, cmdSet, "set", "pratiche.docfa-rossi_persona", "rossi")
	runCmd(t, cmdSet, "set", "pratiche.docfa-rossi_stato", "annullata")

	// No period for scans yet: they aren't checked, and the output says so.
	out := runCmd(t, cmdExpiring, "expiring")
	for _, want := range []string{"procura (rossi)", soon, "No retention period set", "No period set for clients' ID scans"} {
		if !strings.Contains(out, want) {
			t.Errorf("expiring without settings: missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "scans (rossi)") {
		t.Errorf("expiring listed scans with no period set:\n%s", out)
	}
	runCmd(t, cmdRetention, "retention", "scans", "0")
	if out := runCmd(t, cmdRetention, "retention"); !strings.Contains(out, "kept 0 days after the last pratica") {
		t.Errorf("retention after scans 0: %s", out)
	}
	out = runCmd(t, cmdExpiring, "expiring")
	for _, want := range []string{"cie scans (rossi)", "their pratiche are closed"} {
		if !strings.Contains(out, want) {
			t.Errorf("expiring with scans 0: missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "No period set for clients' ID scans") {
		t.Errorf("hint shown with the period set:\n%s", out)
	}
	// The procura is still in force (it expires soon), so its scan waits.
	if strings.Contains(out, "procura scans (rossi)") {
		t.Errorf("expiring listed the scan of a procura still in force:\n%s", out)
	}
	if strings.Contains(out, "verdi") || strings.Contains(out, "data of") {
		t.Errorf("expiring listed verdi or retention without a setting:\n%s", out)
	}
	if !strings.Contains(out, today) {
		t.Errorf("scans due should be dated today (%s):\n%s", today, out)
	}

	if out := runCmd(t, cmdRetention, "retention"); !strings.Contains(out, "No retention period set") {
		t.Errorf("retention before set: %s", out)
	}
	runCmd(t, cmdRetention, "retention", "set", "10")
	if out := runCmd(t, cmdRetention, "retention"); !strings.Contains(out, "kept 10 years") {
		t.Errorf("retention after set: %s", out)
	}
	out = runCmd(t, cmdExpiring, "expiring")
	t.Logf("pvault expiring:\n%s", out)
	if !strings.Contains(out, "data of rossi") || !strings.Contains(out, "2025-01-10") || !strings.Contains(out, "retention ended") || strings.Contains(out, "No retention period set") {
		t.Errorf("expiring with retention:\n%s", out)
	}

	runCmd(t, cmdRetention, "retention", "scans", "30")
	if out := runCmd(t, cmdExpiring, "expiring"); !strings.Contains(out, "cie scans (rossi)") || !strings.Contains(out, "scans due in 30 days") ||
		!strings.Contains(out, time.Now().AddDate(0, 0, 30).Format("2006-01-02")) {
		t.Errorf("expiring with scans 30:\n%s", out)
	}
	runCmd(t, cmdRetention, "retention", "scans", "unset")
	if out := runCmd(t, cmdExpiring, "expiring"); strings.Contains(out, "scans (rossi)") {
		t.Errorf("expiring after scans unset:\n%s", out)
	}

	to := t.TempDir()
	out = runCmd(t, cmdPersonExport, "person", "export", "rossi", "--to", to)
	if !strings.Contains(out, "5 fields, 2 documents") || !strings.Contains(out, "NOT encrypted") {
		t.Errorf("export output:\n%s", out)
	}
	dir := filepath.Join(to, "rossi-export-"+today)
	data, err := os.ReadFile(filepath.Join(dir, "fields.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Person    string
		Fields    []vault.FieldInfo
		Documents []struct {
			Category string `json:"category"`
			File     string `json:"file"`
		}
	}
	if err := json.Unmarshal(data, &index); err != nil || index.Person != "rossi" || len(index.Fields) != 5 || len(index.Documents) != 2 {
		t.Fatalf("fields.json: %v\n%s", err, data)
	}
	for _, d := range index.Documents {
		got, err := os.ReadFile(filepath.Join(dir, d.File))
		if err != nil || string(got) != d.Category {
			t.Errorf("%s = %q, %v", d.File, got, err)
		}
	}
	if strings.Contains(string(data), "verdi") || strings.Contains(string(data), "Studio Test") {
		t.Errorf("export holds another person's or the business's data:\n%s", data)
	}
	entries, _ := v.AuditLog(20)
	var actions []string
	for _, e := range entries {
		actions = append(actions, e.Action)
	}
	if a := strings.Join(actions, " "); !strings.Contains(a, "member_export") || !strings.Contains(a, "retention_set") || !strings.Contains(a, "scan_days_set") {
		t.Errorf("audit log: %s", a)
	}
}
