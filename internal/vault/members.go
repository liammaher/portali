package vault

import (
	"errors"
	"fmt"
	"os"
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

// ErrMemberNotFound means no family member has that slug.
var ErrMemberNotFound = errors.New("member not found")

// checkPersonRefs refuses a value naming a member who isn't in the vault
// (famiglia.genitori, famiglia.intestatario_scheda, pratiche persona).
func (v *Vault) checkPersonRefs(id, value string) error {
	_, local := SplitMember(id)
	parts := strings.SplitN(strings.ToLower(local), ".", 2)
	if len(parts) != 2 || !isPersonRefField(parts[0], parts[1]) {
		return nil
	}
	for _, ref := range PersonRefs(value) {
		if ref == OwnerRef {
			continue
		}
		ok, err := v.db.MemberExists(ref)
		if err != nil {
			return err
		}
		if !ok {
			return &ValidationError{Field: id, Msg: "there is no member \"" + ref + "\" yet: add them first with 'pvault member add " + ref + " --relazione ...'"}
		}
	}
	return nil
}

// isPersonRefField reports the fields whose value names people: those the
// schema marks PersonRef, and a pratica's persona. category and name are
// lower-case.
func isPersonRefField(category, name string) bool {
	if f := GetSchemaField(category + "." + name); f != nil && f.PersonRef {
		return true
	}
	_, campo, ok := praticaField(category, name)
	return ok && campo == "persona"
}

// MemberInfo describes a family member as far as the caller may see:
// derived values appear only when the fields they come from are visible.
type MemberInfo struct {
	Slug      string            `json:"slug"`
	Famiglia  map[string]string `json:"famiglia,omitempty"` // famiglia fields by name
	FirstName string            `json:"first_name,omitempty"`
	LastName  string            `json:"last_name,omitempty"`
	Age       *int              `json:"age,omitempty"`   // from identity.date_of_birth
	Minor     *bool             `json:"minor,omitempty"` // under 18
	// ANPRRelazione is the relazione's code in ANPR Tabella 05, and
	// RelazioneLabel the word forms use (moglie, figlia; neutral without
	// identity.sex).
	ANPRRelazione  string `json:"anpr_relazione,omitempty"`
	RelazioneLabel string `json:"relazione_label,omitempty"`
	Fields         int    `json:"fields"`
	Documents      int    `json:"documents"`
}

// Household lists the vault's family members.
type Household struct {
	IntestatarioScheda string       `json:"intestatario_scheda,omitempty"`
	Members            []MemberInfo `json:"members"`
}

// householdField reports the fields Household decrypts.
func householdField(id string) bool {
	member, local := SplitMember(id)
	if member == "" {
		return local == "famiglia.intestatario_scheda"
	}
	if strings.HasPrefix(local, "famiglia.") {
		return true
	}
	switch local {
	case "identity.first_name", "identity.last_name", "identity.date_of_birth", "identity.sex":
		return true
	}
	return false
}

// Household returns the members whose data allow accepts at least one row
// of, with the famiglia and name fields allow accepts.
func (v *Vault) Household(allow FieldFilter, now time.Time) (*Household, error) {
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

	fields, err := v.db.ListFields()
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if slug, _ := SplitMember(f.Category); slug != "" && allow.allows(f) {
			member(slug).Fields++
		}
	}
	docs, err := v.db.ListDocuments()
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		if slug, _ := SplitMember(d.Category); slug != "" && (allow == nil || allow(DocumentScopeID(d.Category), d.Sensitivity)) {
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
			slug, local := SplitMember(f.ID)
			if slug == "" {
				h.IntestatarioScheda = f.Value
				continue
			}
			if values[slug] == nil {
				values[slug] = map[string]string{}
			}
			values[slug][local] = f.Value
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
		}
		m.FirstName, m.LastName = vals["identity.first_name"], vals["identity.last_name"]
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
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*:famiglia.*", Action: "household"})
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

// MemberInUseError lists the fields that still name a member being removed.
type MemberInUseError struct {
	Slug string
	Refs []string
}

func (e *MemberInUseError) Error() string {
	return fmt.Sprintf("%s is still named in %s: change or delete those first", e.Slug, strings.Join(e.Refs, ", "))
}

// RemovedMember reports what RemoveMember deleted.
type RemovedMember struct {
	Fields        int      `json:"fields"`
	Documents     int      `json:"documents"`
	RevokedTokens []string `json:"revoked_tokens"` // consumers of service tokens that named the member
	DroppedGrants int      `json:"dropped_grants"`
}

// RemoveMember deletes every field and document of a family member, in one
// transaction, and revokes the service tokens and scoped grants whose scope
// names them, so a later member with the same slug can't inherit them. It
// refuses while another field names the member (MemberInUseError).
func (v *Vault) RemoveMember(slug string) (*RemovedMember, error) {
	if !ValidMemberSlug(slug) {
		return nil, &ValidationError{Field: "member", Msg: errMemberSlug(slug).Error()}
	}
	// Lock order: backup.mu, then writeMu, then v.mu. Holding backup.mu
	// keeps a backup from snapshotting rows whose files are being removed.
	v.backup.mu.Lock()
	defer v.backup.mu.Unlock()
	v.writeMu.Lock()
	defer v.writeMu.Unlock()
	if err := v.checkUnlocked(); err != nil {
		return nil, err
	}

	fieldIDs, docIDs, err := v.db.MemberRows(slug)
	if err != nil {
		return nil, err
	}
	if len(fieldIDs) == 0 && len(docIDs) == 0 {
		return nil, ErrMemberNotFound
	}
	refs, err := v.personRefsTo(slug)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		return nil, &MemberInUseError{Slug: slug, Refs: refs}
	}
	if err := v.db.ApplyMemberChange(fieldIDs, docIDs, nil); err != nil {
		return nil, err
	}
	for _, id := range docIDs {
		if err := os.Remove(v.documentPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove document file: %w", err)
		}
	}

	out := &RemovedMember{Fields: len(fieldIDs), Documents: len(docIDs), RevokedTokens: []string{}}
	tokens, err := v.db.ListTokensByUsage("service")
	if err != nil {
		return nil, err
	}
	for _, t := range tokens {
		if ScopeNamesMember(t.Scope, slug) {
			if _, err := v.db.DeleteToken(t.TokenStr); err != nil {
				return nil, err
			}
			out.RevokedTokens = append(out.RevokedTokens, t.Consumer)
		}
	}
	v.mu.RLock()
	if v.session != nil {
		out.DroppedGrants = v.session.DropGrantsNaming(slug)
	}
	v.mu.RUnlock()

	v.db.LogAccess(store.AuditEntry{
		Consumer: "vault", Scope: slug + ":*", Action: "member_remove",
		Purpose: fmt.Sprintf("%d fields, %d documents, %d tokens revoked", out.Fields, out.Documents, len(out.RevokedTokens)),
	})
	v.markChanged()
	return out, nil
}

// personRefsTo returns the IDs of fields outside the member's own data whose
// value names slug. A closed pratica (completata, respinta, annullata) is
// history, not a reference: it doesn't keep a member from being removed.
func (v *Vault) personRefsTo(slug string) ([]string, error) {
	ctx, err := v.decryptFields(func(id, _ string) bool {
		member, local := SplitMember(id)
		parts := strings.SplitN(strings.ToLower(local), ".", 2)
		if member == slug || len(parts) != 2 {
			return false
		}
		_, campo, _ := praticaField(parts[0], parts[1])
		return isPersonRefField(parts[0], parts[1]) || campo == "stato"
	})
	if err != nil {
		return nil, err
	}
	closed := map[string]bool{}
	for _, f := range ctx.Categories["pratiche"] {
		if pslug, campo, _ := praticaField(f.Category, f.FieldName); campo == "stato" && isClosedStato(f.Value) {
			closed[pslug] = true
		}
	}
	var refs []string
	for _, fs := range ctx.Categories {
		for _, f := range fs {
			pslug, campo, isPratica := praticaField(f.Category, f.FieldName)
			if campo == "stato" || (isPratica && closed[pslug]) {
				continue
			}
			if slices.Contains(PersonRefs(f.Value), slug) {
				refs = append(refs, f.ID)
			}
		}
	}
	sort.Strings(refs)
	return refs, nil
}

// Migration of the old nucleo_familiare.membro<N>_<campo> fields.

var reMembroField = regexp.MustCompile(`(?i)^membro([0-9]+)_(.+)$`)

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

// MigrateNucleo turns nucleo_familiare.membro<N>_<campo> fields into family
// members (membro1_nome Giulia becomes giulia:identity.first_name), in one
// transaction. Tiers are never lowered; a field that can't move (unknown
// campo, invalid value, target already set) stays where it is and is
// reported. With dryRun nothing is written.
func (v *Vault) MigrateNucleo(dryRun bool) (*MigrationReport, error) {
	v.backup.mu.Lock()
	defer v.backup.mu.Unlock()
	v.writeMu.Lock()
	defer v.writeMu.Unlock()
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}
	defer clear(vaultKey)

	ctx, err := v.decryptFields(func(id, _ string) bool { return strings.HasPrefix(strings.ToLower(id), "nucleo_familiare.") })
	if err != nil {
		return nil, err
	}
	var old []FieldInfo // every spelling of the category (IDs ignore case elsewhere too)
	for _, fs := range ctx.Categories {
		old = append(old, fs...)
	}
	report := &MigrationReport{DryRun: dryRun, Members: []MigratedMember{}, Moved: map[string]string{}, Deleted: []string{}, Left: map[string]string{}}

	// Group the membro<N> fields and pick each member's slug.
	byN := map[int][]FieldInfo{}
	campi := map[string]string{} // old ID -> lower-case campo
	for _, f := range old {
		if m := reMembroField.FindStringSubmatch(f.FieldName); m != nil {
			n, _ := strconv.Atoi(m[1])
			byN[n] = append(byN[n], f)
			campi[f.ID] = strings.ToLower(m[2])
		}
	}
	existing, err := v.db.MemberSlugs()
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, s := range existing {
		taken[s] = true
	}
	ns := make([]int, 0, len(byN))
	for n := range byN {
		ns = append(ns, n)
	}
	sort.Ints(ns)

	var inserts []store.Field
	var deletes []string
	planned := map[string]bool{}
	now := time.Now()
	move := func(f FieldInfo, newID string) bool {
		value, err := NormalizeAndValidate(newID, f.Value)
		if err != nil {
			report.Left[f.ID] = errMsg(err)
			return false
		}
		if existing, err := v.db.GetField(newID); err != nil || existing != nil || planned[newID] {
			report.Left[f.ID] = newID + " is already set"
			return false
		}
		planned[newID] = true
		tier := f.Sensitivity
		if d := DefaultSensitivity(newID); tierRank[d] > tierRank[tier] {
			tier = d
		}
		if IsCredentialField(newID) {
			tier = TierPersonal
		}
		category, fieldName, _ := strings.Cut(newID, ".")
		encrypted, err := encryptValue(vaultKey, v.salt, category, value)
		if err != nil {
			report.Left[f.ID] = err.Error()
			return false
		}
		inserts = append(inserts, store.Field{ID: newID, Category: category, FieldName: fieldName, Value: encrypted, Sensitivity: tier, UpdatedAt: now})
		deletes = append(deletes, f.ID)
		return true
	}

	for _, n := range ns {
		fields := byN[n]
		slug := ""
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
			report.Members = append(report.Members, mm)
		}
	}

	for _, f := range old {
		switch strings.ToLower(f.FieldName) {
		case "numero_componenti":
			// Derived now: you plus the members in your stato di famiglia.
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

	if dryRun || len(deletes) == 0 {
		return report, nil
	}
	if err := v.db.ApplyMemberChange(deletes, nil, inserts); err != nil {
		return nil, err
	}
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "nucleo_familiare.*", Action: "member_migrate",
		Purpose: fmt.Sprintf("%d members, %d fields moved", len(report.Members), len(inserts))})
	v.markChanged()
	return report, nil
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
	if !ValidMemberSlug(s) {
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
