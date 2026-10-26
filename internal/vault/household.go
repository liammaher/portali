package vault

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/lovincyrus/personal-vault/internal/crypto"
	"github.com/lovincyrus/personal-vault/internal/store"
)

// MemberInfo describes a person in the vault (a family member, or in a
// business vault someone with a ruolo) as far as the caller may see: derived
// values appear only when the fields they come from are visible.
type MemberInfo struct {
	Slug      string            `json:"slug"`
	Famiglia  map[string]string `json:"famiglia,omitempty"` // famiglia fields by name
	Ruolo     map[string]string `json:"ruolo,omitempty"`    // ruolo fields by name (business vaults)
	FirstName string            `json:"first_name,omitempty"`
	LastName  string            `json:"last_name,omitempty"`
	// Denominazione is a company's name (business.denominazione), e.g. a
	// client that is an SRL.
	Denominazione string `json:"denominazione,omitempty"`
	Age           *int   `json:"age,omitempty"`   // from identity.date_of_birth
	Minor         *bool  `json:"minor,omitempty"` // under 18
	// ANPRRelazione is the relazione's code in ANPR Tabella 05, and
	// RelazioneLabel the word forms use (moglie, figlia; neutral without
	// identity.sex).
	ANPRRelazione  string `json:"anpr_relazione,omitempty"`
	RelazioneLabel string `json:"relazione_label,omitempty"`
	Fields         int    `json:"fields"`
	Documents      int    `json:"documents"`
}

// Household lists the vault's other people: family members, or in a
// business vault its people with their ruolo.
type Household struct {
	IntestatarioScheda string       `json:"intestatario_scheda,omitempty"`
	Members            []MemberInfo `json:"members"`
}

// householdField reports the fields Household decrypts.
func householdField(id string) bool {
	member, local := SplitPerson(id)
	if member == "" {
		return local == "famiglia.intestatario_scheda"
	}
	if strings.HasPrefix(local, "famiglia.") || strings.HasPrefix(local, "ruolo.") {
		return true
	}
	switch local {
	case "identity.first_name", "identity.last_name", "identity.date_of_birth", "identity.sex", "business.denominazione":
		return true
	}
	return false
}

// Household returns the people whose data allow accepts at least one row
// of, or with everyone (the session and grants the user approved) every
// person, also those with no rows yet; with the famiglia, ruolo and name
// fields allow accepts.
func (v *Vault) Household(allow FieldFilter, everyone bool, now time.Time) (*Household, error) {
	if err := v.checkUnlocked(); err != nil {
		return nil, err
	}
	members := map[string]*MemberInfo{}
	member := func(slug string) *MemberInfo {
		if members[slug] == nil {
			members[slug] = &MemberInfo{Slug: slug}
		}
		return members[slug]
	}

	if everyone {
		slugs, err := v.db.PersonSlugs()
		if err != nil {
			return nil, err
		}
		for _, slug := range slugs {
			member(slug)
		}
	}
	fields, err := v.db.ListFields()
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if slug, _ := SplitPerson(f.Category); slug != "" && allow.allows(f) {
			member(slug).Fields++
		}
	}
	docs, err := v.db.ListDocuments()
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		if slug, _ := SplitPerson(d.Category); slug != "" && (allow == nil || allow(DocumentScopeID(d.Category), d.Sensitivity)) {
			member(slug).Documents++
		}
	}

	ctx, err := v.decryptFields(func(id, tier string) bool {
		return householdField(id) && (allow == nil || allow(id, tier))
	})
	if err != nil {
		return nil, err
	}
	h := &Household{Members: []MemberInfo{}}
	values := map[string]map[string]string{} // slug -> local ID -> value
	for _, fs := range ctx.Categories {
		for _, f := range fs {
			slug, local := SplitPerson(f.ID)
			value := f.Value
			if !everyone && isPersonRefID(local) {
				if value, err = v.visibleRefs(value, allow); err != nil {
					return nil, err
				}
			}
			if slug == "" {
				h.IntestatarioScheda = value
				continue
			}
			if values[slug] == nil {
				values[slug] = map[string]string{}
			}
			values[slug][local] = value
		}
	}

	for slug, vals := range values {
		m := members[slug]
		if m == nil {
			continue // only reachable through rows counted above
		}
		for local, value := range vals {
			if name, ok := strings.CutPrefix(local, "famiglia."); ok {
				if m.Famiglia == nil {
					m.Famiglia = map[string]string{}
				}
				m.Famiglia[name] = value
			}
			if name, ok := strings.CutPrefix(local, "ruolo."); ok {
				if m.Ruolo == nil {
					m.Ruolo = map[string]string{}
				}
				m.Ruolo[name] = value
			}
		}
		m.FirstName, m.LastName = vals["identity.first_name"], vals["identity.last_name"]
		m.Denominazione = vals["business.denominazione"]
		if dob, ok := vals["identity.date_of_birth"]; ok {
			if age, ok := ageOn(dob, now); ok {
				minor := age < 18
				m.Age, m.Minor = &age, &minor
			}
		}
		if rel, ok := vals["famiglia.relazione"]; ok {
			m.ANPRRelazione = RelazioneANPR(rel)
			m.RelazioneLabel = RelazioneLabel(rel, vals["identity.sex"])
		}
	}
	for _, m := range members {
		h.Members = append(h.Members, *m)
	}
	sort.Slice(h.Members, func(i, j int) bool { return h.Members[i].Slug < h.Members[j].Slug })
	scope := "*:famiglia.*"
	if v.kind == KindImpresa {
		scope = "*:ruolo.*"
	}
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: scope, Action: "household"})
	return h, nil
}

// ageOn returns the age in whole years on now of someone born on dob.
func ageOn(dob string, now time.Time) (int, bool) {
	d, err := NormalizeDate(dob)
	if err != nil {
		return 0, false
	}
	born, _ := time.Parse(dateLayout, d)
	age := now.Year() - born.Year()
	if now.Month() < born.Month() || (now.Month() == born.Month() && now.Day() < born.Day()) {
		age--
	}
	return age, age >= 0
}

// Migration of the old nucleo_familiare.membro<N>_<campo> fields.

var reMembroField = regexp.MustCompile(`^membro([0-9]+)_(.+)$`)

// membroCampi maps an old membro<N>_<campo> to the member's field.
var membroCampi = map[string]string{
	"nome": "identity.first_name", "cognome": "identity.last_name",
	"codice_fiscale": "identity.codice_fiscale", "data_nascita": "identity.date_of_birth",
	"luogo_nascita": "identity.birth_comune", "comune_nascita": "identity.birth_comune",
	"sesso": "identity.sex", "relazione": "famiglia.relazione",
}

// MigratedMember is one membro<N> turned into a member.
type MigratedMember struct {
	N      int               `json:"n"`
	Slug   string            `json:"slug"`
	Fields map[string]string `json:"fields"` // old ID -> new ID
}

// MigrationReport says what MigrateNucleo did (or would do, in a dry run).
type MigrationReport struct {
	DryRun  bool              `json:"dry_run"`
	Members []MigratedMember  `json:"members"`
	Moved   map[string]string `json:"moved"`   // owner fields: old ID -> new ID
	Deleted []string          `json:"deleted"` // dropped without a new field
	Left    map[string]string `json:"left"`    // old ID -> why it stays
}

// metaNucleoMembro + N records whom MigrateNucleo made nucleo_familiare's membro<N>.
const metaNucleoMembro = "nucleo_membro"

// MigrateNucleo turns nucleo_familiare.membro<N>_<campo> fields into family
// members (membro1_nome Giulia becomes giulia:identity.first_name), in one
// transaction. Tiers are never lowered; a field that can't move (unknown
// campo, invalid value, target already set) stays where it is and is
// reported. With dryRun nothing is written.
func (v *Vault) MigrateNucleo(dryRun bool) (*MigrationReport, error) {
	if v.kind != KindPersona {
		return nil, &ValidationError{Field: "nucleo_familiare", Msg: "a business vault has no family to migrate: famiglia is for personal vaults"}
	}
	v.backup.mu.Lock()
	defer v.backup.mu.Unlock()
	v.writeMu.Lock()
	defer v.writeMu.Unlock()
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}
	defer clear(vaultKey)

	ctx, err := v.decryptFields(func(id, _ string) bool { return strings.HasPrefix(id, "nucleo_familiare.") })
	if err != nil {
		return nil, err
	}
	old := ctx.Categories["nucleo_familiare"]
	report := &MigrationReport{DryRun: dryRun, Members: []MigratedMember{}, Moved: map[string]string{}, Deleted: []string{}, Left: map[string]string{}}

	// Group the membro<N> fields and pick each member's slug.
	byN := map[int][]FieldInfo{}
	campi := map[string]string{} // old ID -> lower-case campo
	for _, f := range old {
		if m := reMembroField.FindStringSubmatch(f.FieldName); m != nil {
			n, _ := strconv.Atoi(m[1])
			byN[n] = append(byN[n], f)
			campi[f.ID] = m[2]
		}
	}
	existing, err := v.db.PersonSlugs()
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, s := range existing {
		taken[s] = true
	}
	// A slug a pratica still names belonged to someone removed: not for a new member.
	named, err := v.praticheNaming()
	if err != nil {
		return nil, err
	}
	for s := range named {
		taken[s] = true
	}
	// Who an earlier run made each membro, so fields it left behind (fixed
	// since) join that person instead of a new one named membro<N>. Kept only
	// while that membro has fields left, and used only for a group without a
	// nome: a nome there is someone entered since, a new person.
	migrated, err := v.db.MetaWithPrefix(metaNucleoMembro)
	if err != nil {
		return nil, err
	}
	ns := make([]int, 0, len(byN))
	for n := range byN {
		ns = append(ns, n)
	}
	sort.Ints(ns)

	var inserts []store.Field
	var deletes []string
	planned := map[string]bool{}
	inStato, err := v.countInStatoFamiglia() // the people already marked, before this migration
	if err != nil {
		return nil, err
	}
	now := time.Now()
	// hasPerson reports someone who exists, or is a member this run adds.
	hasPerson := func(slug string) bool {
		return slices.Contains(existing, slug) ||
			slices.ContainsFunc(report.Members, func(m MigratedMember) bool { return m.Slug == slug })
	}
	var dbErr error // a failed lookup ends the migration, not just one field
	// add plans a new field, never below its schema default tier or minTier.
	add := func(newID, value, minTier string) error {
		value, err := NormalizeAndValidate(newID, value)
		if err != nil {
			return errors.New(errMsg(err))
		}
		// As checkPersonRefs: a value naming someone who doesn't exist would
		// name whoever is later given that slug.
		_, local := SplitPerson(newID)
		if category, name, _ := strings.Cut(canonicalLocalID(local), "."); isPersonRefField(category, name) {
			for _, ref := range PersonRefs(value) {
				if ref != OwnerRef && !hasPerson(ref) {
					return errors.New("there is no person \"" + ref + "\": add them with 'pvault person add', then migrate again")
				}
			}
		}
		existingField, err := v.db.GetField(newID)
		if err != nil {
			dbErr = err
			return err
		}
		if existingField != nil || planned[newID] {
			return errors.New(newID + " is already set")
		}
		planned[newID] = true
		tier := minTier
		if d := DefaultSensitivity(newID); tierRank[d] > tierRank[tier] {
			tier = d
		}
		if IsCredentialField(newID) {
			tier = TierPersonal
		}
		category, fieldName, _ := strings.Cut(newID, ".")
		encrypted, err := encryptValue(vaultKey, v.salt, category, value)
		if err != nil {
			return err
		}
		inserts = append(inserts, store.Field{ID: newID, Category: category, FieldName: fieldName, Value: encrypted, Sensitivity: tier, UpdatedAt: now})
		return nil
	}
	move := func(f FieldInfo, newID string) bool {
		if err := add(newID, f.Value, f.Sensitivity); err != nil {
			report.Left[f.ID] = err.Error()
			return false
		}
		deletes = append(deletes, f.ID)
		return true
	}

	slugOf := map[int]string{}
	for _, n := range ns {
		fields := byN[n]
		hasNome := slices.ContainsFunc(fields, func(f FieldInfo) bool { return campi[f.ID] == "nome" })
		slug := migrated[metaNucleoMembro+strconv.Itoa(n)]
		if hasNome || !slices.Contains(existing, slug) {
			slug = ""
			for _, f := range fields {
				if campi[f.ID] == "nome" {
					slug = slugFromName(f.Value)
				}
			}
			if slug == "" {
				slug = fmt.Sprintf("membro%d", n)
			}
			base := slug
			for i := 2; taken[slug]; i++ {
				slug = fmt.Sprintf("%s-%d", base, i)
			}
			taken[slug] = true
		}

		slugOf[n] = slug
		mm := MigratedMember{N: n, Slug: slug, Fields: map[string]string{}}
		for _, f := range fields {
			target, ok := membroCampi[campi[f.ID]]
			if !ok {
				report.Left[f.ID] = "no matching member field; set it by hand as " + slug + ":<category>.<field>"
				continue
			}
			if newID := slug + ":" + target; move(f, newID) {
				mm.Fields[f.ID] = newID
			}
		}
		if len(mm.Fields) > 0 {
			// Being in nucleo_familiare meant being in the stato di famiglia.
			if add(slug+":famiglia.stato_famiglia", "si", "standard") == nil {
				inStato++
			}
			report.Members = append(report.Members, mm)
		}
	}

	for _, f := range old {
		switch f.FieldName {
		case "numero_componenti":
			// Derived now: you plus the members in your stato di famiglia. A
			// count that doesn't match stays, so it isn't lost.
			if derived := strconv.Itoa(1 + inStato); strings.TrimSpace(f.Value) != derived {
				report.Left[f.ID] = "you plus the members in your stato di famiglia make " + derived +
					": check each one's famiglia.stato_famiglia, then delete this field"
				continue
			}
			deletes = append(deletes, f.ID)
			report.Deleted = append(report.Deleted, f.ID)
		case "intestatario_scheda":
			if move(f, "famiglia.intestatario_scheda") {
				report.Moved[f.ID] = "famiglia.intestatario_scheda"
			}
		default:
			if !reMembroField.MatchString(f.FieldName) {
				report.Left[f.ID] = "no matching field in the famiglia category"
			}
		}
	}

	if dbErr != nil {
		return nil, dbErr
	}
	if dryRun || len(deletes) == 0 {
		return report, nil
	}
	// Remember who each membro with fields left became; forget the rest.
	remember := map[string]string{}
	for n, fields := range byN {
		left := slices.ContainsFunc(fields, func(f FieldInfo) bool { return report.Left[f.ID] != "" })
		made := slices.Contains(existing, slugOf[n]) ||
			slices.ContainsFunc(report.Members, func(m MigratedMember) bool { return m.Slug == slugOf[n] })
		if left && made {
			remember[metaNucleoMembro+strconv.Itoa(n)] = slugOf[n]
		}
	}
	var forget []string
	for k := range migrated {
		if _, ok := remember[k]; !ok {
			forget = append(forget, k)
		}
	}
	if err := v.db.ApplyChange(store.Change{DeleteFields: deletes, Insert: inserts, SetMeta: remember, DeleteMeta: forget}); err != nil {
		return nil, err
	}
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "nucleo_familiare.*", Action: "member_migrate",
		Purpose: fmt.Sprintf("%d members, %d fields moved", len(report.Members), len(inserts))})
	v.markChanged()
	return report, nil
}

// countInStatoFamiglia counts the people whose famiglia.stato_famiglia is si.
func (v *Vault) countInStatoFamiglia() (int, error) {
	ctx, err := v.decryptFields(func(id, _ string) bool {
		person, rest := SplitPerson(id)
		return person != "" && rest == "famiglia.stato_famiglia"
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, fields := range ctx.Categories {
		for _, f := range fields {
			if f.Value == "si" {
				n++
			}
		}
	}
	return n, nil
}

// encryptValue encrypts a field value with its category's subkey.
func encryptValue(vaultKey, salt []byte, category, value string) (string, error) {
	subkey, err := crypto.DeriveSubkey(vaultKey, salt, category)
	if err != nil {
		return "", fmt.Errorf("derive subkey: %w", err)
	}
	defer clear(subkey)
	return crypto.EncryptToBase64(subkey, []byte(value))
}

// decryptValue decrypts a field value stored under category's subkey.
func decryptValue(vaultKey, salt []byte, category, encrypted string) (string, error) {
	subkey, err := crypto.DeriveSubkey(vaultKey, salt, category)
	if err != nil {
		return "", fmt.Errorf("derive subkey: %w", err)
	}
	defer clear(subkey)
	plaintext, err := crypto.DecryptFromBase64(subkey, encrypted)
	defer clear(plaintext)
	return string(plaintext), err
}

// errMsg is a ValidationError's message without the field ID.
func errMsg(err error) string {
	var verr *ValidationError
	if errors.As(err, &verr) {
		return verr.Msg
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// slugFromName makes a member slug from a first name: "Maria Luisa" becomes
// maria-luisa, accents are dropped. "" if nothing usable is left.
func slugFromName(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		r = foldAccent(r)
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9' && b.Len() > 0:
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash && (unicode.IsSpace(r) || r == '-' || r == '\'' || r == '_'):
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimSuffix(b.String(), "-")
	if len(s) > 32 {
		s = strings.TrimSuffix(s[:32], "-")
	}
	if !ValidPersonSlug(s) {
		return ""
	}
	return s
}

// foldAccent maps accented Latin letters to their base letter.
func foldAccent(r rune) rune {
	const from, to = "àáâäãåèéêëìíîïòóôöõùúûüçñýÿ", "aaaaaaeeeeiiiiooooouuuucnyy"
	if i := strings.IndexRune(from, r); i >= 0 {
		return []rune(to)[len([]rune(from[:i]))]
	}
	return r
}
