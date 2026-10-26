package main

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/lovincyrus/personal-vault/internal/vault"
	"golang.org/x/term"
)

const personUsage = `usage:
  pvault person list
  pvault person show <slug>
  pvault person add <slug> --relazione <relazione> [--nome N] [--cognome C] [--codice-fiscale CF]
                    [--data-nascita YYYY-MM-DD] [--sesso M|F]
      e.g. pvault person add giulia --relazione moglie --nome Giulia
  pvault --vault <name> person add <slug> --ruolo <ruolo> [--nome N] [--cognome C] [--denominazione D] ...
      in a business vault, e.g. pvault --vault studio person add rossi --ruolo cliente --nome Mario
  pvault person remove <slug>
  pvault person export <slug> [--to <folder>] [--include-personal]
  pvault person migrate [--dry-run]
Another person's fields are <slug>:<category>.<field>, e.g. pvault set leo:cie.numero CA12345AB.
(pvault member is an alias for pvault person.)`

func cmdPerson() {
	if len(os.Args) < 3 {
		fatal(personUsage)
	}
	switch os.Args[2] {
	case "list":
		positionalArgs(os.Args[3:], personUsage, 0, 0)
		cmdPersonList()
	case "show":
		cmdPersonShow(positionalArgs(os.Args[3:], personUsage, 1, 1)[0])
	case "add":
		cmdPersonAdd()
	case "remove":
		cmdPersonRemove(positionalArgs(os.Args[3:], personUsage, 1, 1)[0])
	case "export":
		cmdPersonExport()
	case "migrate":
		cmdPersonMigrate()
	default:
		fatal(personUsage)
	}
}

// checkSlug fails unless slug is a valid person slug.
func checkSlug(slug string) {
	if !vault.ValidPersonSlug(slug) {
		fatal("invalid person %q: lower-case letters, digits and hyphens, starting with a letter (\"io\" is you)", slug)
	}
}

func fetchHousehold() (*vault.Household, error) {
	resp, err := apiRequest("GET", "/vault/members", nil)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	var h vault.Household
	if err := apiResult(resp, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func cmdPersonList() {
	kind, err := serverKind()
	if err != nil {
		fatal("%v", err)
	}
	if kind == vault.KindImpresa {
		needGrant("*:ruolo.*,*:identity.*,*:business.denominazione", "read")
	} else {
		needGrant("*:famiglia.*,*:identity.*", "read")
	}
	h, err := fetchHousehold()
	if err != nil {
		fatal("%v", err)
	}
	fmt.Print(formatHousehold(h, kind))
}

// formatHousehold prints one line per person: their relazione (a personal
// vault) or ruolo (a business vault), name and the facts that matter.
func formatHousehold(h *vault.Household, kind string) string {
	if kind == vault.KindImpresa {
		return formatBusinessPeople(h)
	}
	if len(h.Members) == 0 {
		return "No family members yet. Add one with 'pvault person add <slug> --relazione <relazione>'.\n"
	}
	var b strings.Builder
	width := len("slug")
	for _, m := range h.Members {
		width = max(width, len(m.Slug))
	}
	for _, m := range h.Members {
		rel := m.RelazioneLabel
		if rel == "" {
			rel = "(no relazione)"
		}
		parts := []string{rel}
		if name := strings.TrimSpace(m.FirstName + " " + m.LastName); name != "" {
			parts = append(parts, name)
		}
		if m.Age != nil {
			age := fmt.Sprintf("%d anni", *m.Age)
			if *m.Minor {
				age += " (minore)"
			}
			parts = append(parts, age)
		}
		if m.Famiglia["stato_famiglia"] == "si" {
			parts = append(parts, "stato di famiglia")
		}
		if p := m.Famiglia["a_carico_percentuale"]; p != "" && p != "0" {
			parts = append(parts, "a carico "+p+"%")
		}
		if g := m.Famiglia["genitori"]; g != "" {
			parts = append(parts, "genitori "+g)
		}
		// No field counts: under a scoped grant they'd cover only its categories.
		fmt.Fprintf(&b, "%-*s  %s\n", width, m.Slug, strings.Join(parts, " · "))
	}
	if h.IntestatarioScheda != "" {
		fmt.Fprintf(&b, "Intestatario della scheda anagrafica: %s\n", h.IntestatarioScheda)
	}
	return b.String()
}

func cmdPersonShow(slug string) {
	checkSlug(slug)
	needGrant(slug+":*", "read")
	resp, err := apiRequest("GET", "/vault/context", nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var ctx vault.ContextBundle
	if err := apiResult(resp, &ctx); err != nil {
		fatal("%v", err)
	}
	var fields []vault.FieldInfo
	for cat, fs := range ctx.Categories {
		if person, _ := vault.SplitPerson(cat); person == slug {
			fields = append(fields, fs...)
		}
	}
	// Secrets stay off the screen; 'pvault get <id>' shows one on purpose.
	for i := range fields {
		if fields[i].Sensitivity == vault.TierPersonal {
			fields[i].Value = ""
		}
	}
	if len(fields) == 0 {
		fatal("no fields for %s (see '%s')", slug, pvCmd("person list"))
	}
	printFields(fields)
}

// printFields prints field IDs and values (or versions) in aligned columns,
// sorted by ID.
func printFields(fields []vault.FieldInfo) {
	sort.Slice(fields, func(i, j int) bool { return fields[i].ID < fields[j].ID })
	width := 0
	for _, f := range fields {
		width = max(width, len(f.ID))
	}
	for _, f := range fields {
		sens := ""
		if f.Sensitivity != "" && f.Sensitivity != "standard" {
			sens = fmt.Sprintf(" [%s]", f.Sensitivity)
		}
		if f.Value != "" {
			fmt.Printf("%-*s  %s%s\n", width, f.ID, f.Value, sens)
		} else {
			fmt.Printf("%-*s  (v%d)%s\n", width, f.ID, f.Version, sens)
		}
	}
}

// formatBusinessPeople is formatHousehold for a business vault.
func formatBusinessPeople(h *vault.Household) string {
	if len(h.Members) == 0 {
		return "No people yet. Add one with 'pvault " + vaultFlag() + "person add <slug> --ruolo <ruolo>'.\n"
	}
	var b strings.Builder
	width := len("slug")
	for _, m := range h.Members {
		width = max(width, len(m.Slug))
	}
	for _, m := range h.Members {
		ruolo := strings.ReplaceAll(strings.ReplaceAll(m.Ruolo["tipo"], "_", " "), ",", ", ")
		if ruolo == "" {
			ruolo = "(no ruolo)"
		}
		parts := []string{ruolo}
		name := strings.TrimSpace(m.FirstName + " " + m.LastName)
		if m.Denominazione != "" {
			name = m.Denominazione
		}
		if name != "" {
			parts = append(parts, name)
		}
		if q := m.Ruolo["quota_percentuale"]; q != "" {
			parts = append(parts, "quota "+q+"%")
		}
		if d := m.Ruolo["data_inizio"]; d != "" {
			parts = append(parts, "dal "+d)
		}
		if d := m.Ruolo["data_fine"]; d != "" {
			parts = append(parts, "fino al "+d)
		}
		fmt.Fprintf(&b, "%-*s  %s\n", width, m.Slug, strings.Join(parts, " · "))
	}
	return b.String()
}

// personAddFlags maps person add's options to the person's fields. The
// relation (relazione in a personal vault, ruolo in a business one) is
// written first, so the person exists before the rest.
var personAddFlags = []struct{ flag, field string }{
	{"--relazione", "famiglia.relazione"},
	{"--ruolo", "ruolo.tipo"},
	{"--denominazione", "business.denominazione"},
	{"--nome", "identity.first_name"},
	{"--cognome", "identity.last_name"},
	{"--codice-fiscale", "identity.codice_fiscale"},
	{"--data-nascita", "identity.date_of_birth"},
	{"--sesso", "identity.sex"},
}

// personAddFields reads person add's options into the fields to write, and
// checks every value first, so a bad one (e.g. the codice fiscale) is
// refused before anything is written or any prompt is shown.
func personAddFields(slug, kind string, args []string) ([][2]string, error) {
	values := make([]string, len(personAddFlags))
	flags := map[string]*string{}
	for i, f := range personAddFlags {
		flags[f.flag] = &values[i]
	}
	if err := parseFlags(args, flags); err != nil {
		return nil, err
	}
	relazione, ruolo := values[0], values[1]
	switch {
	case kind == vault.KindImpresa && relazione != "":
		return nil, errors.New("this is a business vault: people have a --ruolo, not a --relazione")
	case kind == vault.KindImpresa && ruolo == "":
		return nil, errors.New("--ruolo is required (e.g. titolare, socio, dipendente, cliente)")
	case kind != vault.KindImpresa && ruolo != "":
		return nil, errors.New("--ruolo is for business vaults; family members have a --relazione")
	case kind != vault.KindImpresa && values[2] != "":
		return nil, errors.New("--denominazione is for business vaults (a company among a business's people)")
	case kind != vault.KindImpresa && relazione == "":
		return nil, errors.New("--relazione is required (e.g. coniuge, figlio, genitore)")
	}
	var out [][2]string
	for i, f := range personAddFlags {
		if values[i] != "" {
			id := slug + ":" + f.field
			if _, err := vault.NormalizeAndValidate(id, values[i]); err != nil {
				return nil, fmt.Errorf("%s: %w", f.flag, err)
			}
			out = append(out, [2]string{id, values[i]})
		}
	}
	return out, nil
}

func cmdPersonAdd() {
	if len(os.Args) < 4 {
		fatal(personUsage)
	}
	slug := os.Args[3]
	checkSlug(slug)
	kind, err := serverKind()
	if err != nil {
		fatal("%v", err)
	}
	fields, err := personAddFields(slug, kind, os.Args[4:])
	if err != nil {
		fatal("%v\n%s", err, personUsage)
	}
	if kind == vault.KindImpresa {
		needGrant(slug+":ruolo.*,"+slug+":identity.*,"+slug+":business.*", "append")
	} else {
		needGrant(slug+":famiglia.*,"+slug+":identity.*", "append")
	}
	if err := addPerson(slug, fields); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Added %s. Their fields are %s:<category>.<field>, e.g. pvault %sset %s:cie.numero <numero>\n", slug, slug, vaultFlag(), slug)
}

// addPerson writes a new person's fields, refusing a slug that already has
// any of them, so adding never overwrites someone.
func addPerson(slug string, fields [][2]string) error {
	for _, f := range fields {
		resp, err := apiRequest("GET", "/vault/fields/"+f[0], nil)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		switch resp.StatusCode {
		case http.StatusNotFound:
			resp.Body.Close()
		case http.StatusOK:
			resp.Body.Close()
			return fmt.Errorf("%s already has %s: change it with '%s', or pick another slug", slug, f[0], pvCmd("set "+f[0]+" <value>"))
		default:
			return apiResult(resp, nil) // closes the body after reading the server's reason
		}
	}
	for _, f := range fields {
		resp, err := apiRequest("PUT", "/vault/fields/"+f[0], map[string]string{"value": f[1]})
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		if err := apiResult(resp, nil); err != nil {
			return err
		}
	}
	return nil
}

func cmdPersonRemove(slug string) {
	checkSlug(slug)
	if term.IsTerminal(int(syscall.Stdin)) {
		fmt.Fprintf(os.Stderr, "This deletes every field and document of %s and revokes tokens that name them.\nType %s to confirm: ", slug, slug)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != slug {
			fatal("not removed")
		}
	}
	// The approval names exactly this: everything about slug, with write access.
	needGrant(slug+":*", "write")
	resp, err := apiRequest("DELETE", "/vault/members/"+slug, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var out vault.RemovedPerson
	if err := apiResult(resp, &out); err != nil {
		fatal("%v", err)
	}
	fmt.Printf("Removed %s: %d fields, %d documents.\n", slug, out.Fields, out.Documents)
	if len(out.RevokedTokens) > 0 {
		fmt.Printf("Revoked service tokens that named %s: %s\n", slug, strings.Join(out.RevokedTokens, ", "))
	}
}

func cmdPersonMigrate() {
	var dryRun bool
	switch args := os.Args[3:]; {
	case len(args) == 1 && args[0] == "--dry-run":
		dryRun = true
	case len(args) != 0:
		fatal(personUsage)
	}
	needGrant("", "") // only your own session may migrate
	path := "/vault/members/migrate"
	if dryRun {
		path += "?dry_run=1"
	}
	resp, err := apiRequest("POST", path, nil)
	if err != nil {
		fatal("request failed: %v", err)
	}
	var report vault.MigrationReport
	if err := apiResult(resp, &report); err != nil {
		fatal("%v", err)
	}
	fmt.Print(formatMigration(&report))
}

// formatMigration describes a migration report.
func formatMigration(r *vault.MigrationReport) string {
	var b strings.Builder
	if len(r.Members) == 0 && len(r.Moved) == 0 && len(r.Deleted) == 0 && len(r.Left) == 0 {
		return "No nucleo_familiare fields to migrate.\n"
	}
	verb := "Moved"
	if r.DryRun {
		verb = "Would move"
	}
	for _, m := range r.Members {
		fmt.Fprintf(&b, "membro%d → %s\n", m.N, m.Slug)
		for _, old := range sortedKeys(m.Fields) {
			fmt.Fprintf(&b, "  %s %s → %s\n", verb, old, m.Fields[old])
		}
	}
	for _, old := range sortedKeys(r.Moved) {
		fmt.Fprintf(&b, "%s %s → %s\n", verb, old, r.Moved[old])
	}
	for _, id := range r.Deleted {
		fmt.Fprintf(&b, "Drop %s (now counted from the people in the vault)\n", id)
	}
	for _, id := range sortedKeys(r.Left) {
		fmt.Fprintf(&b, "Left %s: %s\n", id, r.Left[id])
	}
	if r.DryRun {
		b.WriteString("Dry run: nothing changed. Run '" + pvCmd("person migrate") + "' to apply.\n")
	}
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}
