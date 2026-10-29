package vault

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/crypto"
	"github.com/lovincyrus/personal-vault/internal/napoli"
	"github.com/lovincyrus/personal-vault/internal/store"
)

// ErrNotLocatable is returned by Locate for a category that is not an address.
var ErrNotLocatable = errors.New("not an address: use residenza, domicilio, locale, immobile or sede_legale")

func auditRead(id string) store.AuditEntry {
	return store.AuditEntry{Consumer: "vault", Scope: id, Action: "read"}
}

// An address in the Comune di Napoli has three fields the vault computes when
// none is stored: its quartiere, municipalità and ASL distretto (see
// internal/napoli). They are read like stored fields, GetIf, GetByCategory and
// GetContext all answer them, but they are never written by the vault: List,
// export and backups hold stored rows only. Storing one overrides the computed
// value, for as long as it is not older than the address it was stored for.
//
// A computed field is read under its own scope and tier (standard), not the
// address's: a token scoped to residenza.municipalita gets the municipalità
// and never sees the street. The audit log records a read of the computed
// field, not of the street.

// locatedKind is one kind of address and the names of its fields.
type locatedKind struct {
	category                       string // schema category
	street, civico, comune         string
	quartiere, municipalita, distr string
}

var locatedKinds = []locatedKind{
	{"residenza", "indirizzo", "civico", "comune", "quartiere", "municipalita", "distretto_asl"},
	{"domicilio", "indirizzo", "civico", "comune", "quartiere", "municipalita", "distretto_asl"},
	{"locale", "indirizzo", "civico", "comune", "quartiere", "municipalita", "distretto_asl"},
	{"immobile", "indirizzo", "civico", "comune", "quartiere", "municipalita", "distretto_asl"},
	{"business", "sede_legale_indirizzo", "sede_legale_civico", "sede_legale_comune",
		"sede_legale_quartiere", "sede_legale_municipalita", "sede_legale_distretto_asl"},
}

// Indexes into a located address's three computed fields, in the order
// LocatedTarget returns them.
const (
	LocQuartiere = iota
	LocMunicipalita
	LocDistretto
)

var locatedLabels = [3]string{"quartiere", "municipalità", "distretto"}

func (k locatedKind) outputs() []string {
	return []string{k.quartiere, k.municipalita, k.distr}
}

// locatedKindOfCategory returns the kind of address a stored category holds
// (residenza, leo:residenza, locale2, business), if it is one.
func locatedKindOfCategory(category string) (locatedKind, bool) {
	if category != strings.ToLower(category) {
		return locatedKind{}, false
	}
	_, base := SplitPerson(category)
	c := schemaCategoryOf(base)
	if c == nil {
		return locatedKind{}, false
	}
	for _, k := range locatedKinds {
		if k.category == c.Name {
			return k, true
		}
	}
	return locatedKind{}, false
}

// locatedOutput reports a field ID that names one of an address's computed
// fields, with the category it is in and which of the three it is.
func locatedOutput(id string) (k locatedKind, category string, which int, ok bool) {
	category, name, found := strings.Cut(id, ".")
	if !found {
		return locatedKind{}, "", 0, false
	}
	if k, ok = locatedKindOfCategory(category); !ok {
		return locatedKind{}, "", 0, false
	}
	which = slices.Index(k.outputs(), name)
	return k, category, which, which >= 0
}

// IsLocatedField reports whether an ID names a field the vault computes
// when none is stored.
func IsLocatedField(id string) bool {
	_, _, _, ok := locatedOutput(id)
	return ok
}

// LocatedTarget reads what a person asks to locate: an address kind with an
// optional person ("residenza", "leo:domicilio", "locale2", "sede_legale").
// It returns the stored category and the IDs of the three computed fields
// (quartiere, municipalità, distretto).
func LocatedTarget(target string) (category string, ids [3]string, ok bool) {
	person, name := SplitPerson(target + ".x")
	name = strings.TrimSuffix(name, ".x")
	if name == "sede_legale" {
		name = "business"
	}
	category = name
	if person != "" {
		category = person + ":" + name
	}
	k, ok := locatedKindOfCategory(category)
	if !ok {
		return "", ids, false
	}
	for i, o := range k.outputs() {
		ids[i] = category + "." + o
	}
	return category, ids, true
}

// LocatedField is one of an address's three fields.
type LocatedField struct {
	Value  string `json:"value"`
	Source string `json:"source"` // "stored" or "computed"
}

// Located says where one address of the vault is. It never holds the street.
type Located struct {
	Category string `json:"category"`
	// Fields are the fields known and visible to the caller, by field ID.
	Fields map[string]LocatedField `json:"fields"`
	// Basis is what settled the computed municipalità: "street", "civico"
	// (a range of the 2003 stradario) or "quartiere".
	Basis string `json:"basis,omitempty"`
	// Partial means the street was matched on fewer words than the stradario's
	// name; worth confirming.
	Partial bool `json:"partial,omitempty"`
	// Choices are the quartieri to ask about when the street crosses a
	// municipalità boundary.
	Choices []string `json:"choices,omitempty"`
	// Reason says why the municipalità is not known.
	Reason string   `json:"reason,omitempty"`
	Notes  []string `json:"notes,omitempty"`
}

// NotResolvedError is returned by a read of a computed field the vault can't
// compute: a street that crosses a municipalità boundary, an address outside
// Napoli, no address.
type NotResolvedError struct {
	Reason  string
	Choices []string
}

func (e *NotResolvedError) Error() string {
	if len(e.Choices) == 0 {
		return e.Reason
	}
	return e.Reason + ": " + strings.Join(e.Choices, ", ")
}

// locatedRow is a stored row read for locating.
type locatedRow struct {
	exists  bool
	value   string
	tier    string
	updated time.Time
}

type locatedOut struct {
	id     string
	value  string // "" when not known
	source string // "stored" or "computed"
	tier   string // the tier the value is read under
	stale  bool   // a stored row exists but is older than the address: ignored
	shown  bool   // the caller may see it
}

type locatedState struct {
	category string
	kind     locatedKind
	updated  time.Time // the newest row of the address
	rows     [3]locatedRow
	out      [3]locatedOut
	res      napoli.Result
	ran      bool // the stradario was asked
	reason   string
	notes    []string
}

// locatedRead reads a stored row of the address, decrypting it with the
// category's key.
func (v *Vault) locatedRead(subkey []byte, id string) (locatedRow, error) {
	f, err := v.db.GetField(id)
	if err != nil || f == nil {
		return locatedRow{}, err
	}
	row := locatedRow{exists: true, tier: f.Sensitivity, updated: f.UpdatedAt}
	plain, err := crypto.DecryptFromBase64(subkey, f.Value)
	if err != nil {
		return locatedRow{}, fmt.Errorf("decrypt field %s: %w", id, err)
	}
	row.value = strings.TrimSpace(string(plain))
	return row, nil
}

// locate works out the three fields of one address (category as stored:
// residenza, leo:residenza, locale2, business) for a caller allow describes.
func (v *Vault) locate(vaultKey []byte, category string, k locatedKind, allow FieldFilter) (*locatedState, error) {
	subkey, err := crypto.DeriveSubkey(vaultKey, v.salt, category)
	if err != nil {
		return nil, err
	}
	defer clear(subkey)

	st := &locatedState{category: category, kind: k}
	outs := k.outputs()
	for i, name := range outs {
		st.out[i].id = category + "." + name
		if st.rows[i], err = v.locatedRead(subkey, st.out[i].id); err != nil {
			return nil, err
		}
	}
	// Whether the caller's tier cap reaches personal items, asked of its own
	// scope (the computed fields): a caller needs no access to the street to be
	// answered, but personal items never feed one that can't see them.
	reachesPersonal := allow == nil
	for _, o := range st.out {
		reachesPersonal = reachesPersonal || allow(o.id, TierPersonal)
	}
	// The address's own rows.
	input := func(name string) (string, error) {
		id := category + "." + name
		r, err := v.locatedRead(subkey, id)
		if err != nil || !r.exists {
			return "", err
		}
		if r.updated.After(st.updated) {
			st.updated = r.updated
		}
		if r.tier == TierPersonal && !reachesPersonal {
			return "", nil
		}
		return r.value, nil
	}
	street, err := input(k.street)
	if err != nil {
		return nil, err
	}
	civico, err := input(k.civico)
	if err != nil {
		return nil, err
	}
	comune, err := input(k.comune)
	if err != nil {
		return nil, err
	}

	// A stored value stands while it is not older than the address (or, for the
	// distretto, than the stored municipalità it follows).
	valid := func(i int, since time.Time) bool {
		r := st.rows[i]
		return r.exists && r.value != "" && !r.updated.Before(since)
	}
	staleSince := [3]time.Time{st.updated, st.updated, st.updated}
	if st.rows[LocMunicipalita].exists && st.rows[LocMunicipalita].updated.After(st.updated) {
		staleSince[LocDistretto] = st.rows[LocMunicipalita].updated
	}
	for i := range st.out {
		r := st.rows[i]
		st.out[i].tier = "standard"
		if r.exists {
			st.out[i].tier = r.tier
			if valid(i, staleSince[i]) {
				st.out[i].value, st.out[i].source = r.value, "stored"
			} else if r.value != "" {
				st.out[i].stale = true
			}
		}
	}

	// Ask the stradario, with the stored quartiere if it still stands.
	differs := ""
	switch {
	case street == "":
		st.reason = "the address has no street"
	case comune == "":
		st.reason = "the address has no comune"
	case !napoli.InNapoli(comune):
		st.reason = "the address is not in Napoli"
	default:
		q := napoli.Query{Street: street, Civico: civico}
		if st.out[LocQuartiere].source == "stored" && (st.rows[LocQuartiere].tier != TierPersonal || reachesPersonal) {
			q.Quartiere = st.out[LocQuartiere].value
		}
		st.res, st.ran = napoli.Lookup(q), true
		switch st.res.Status {
		case napoli.NotFound:
			st.reason = "the street is not in the stradario of Napoli"
		case napoli.Ambiguous:
			st.reason = "the street crosses municipalità boundaries; the quartiere decides"
		}
	}
	if st.ran && st.res.Status == napoli.Resolved {
		if st.out[LocQuartiere].value == "" {
			st.out[LocQuartiere].value, st.out[LocQuartiere].source = st.res.Quartiere, "computed"
		}
		if st.out[LocMunicipalita].value == "" {
			st.out[LocMunicipalita].value, st.out[LocMunicipalita].source = strconv.Itoa(st.res.Municipalita), "computed"
		} else if stored := st.out[LocMunicipalita].value; stored != strconv.Itoa(st.res.Municipalita) {
			differs = "the stored municipalità (" + stored + ") differs from the stradario's (" + strconv.Itoa(st.res.Municipalita) + ")"
		}
	}

	// What the caller may see. A row it may not see is no reason to show the
	// computed value in its place, and nothing computed from a municipalità it
	// may not see.
	for i := range st.out {
		st.out[i].shown = allow == nil || allow(st.out[i].id, st.out[i].tier)
	}
	if st.out[LocDistretto].value == "" {
		if st.rows[LocMunicipalita].exists && !st.out[LocMunicipalita].shown {
			st.out[LocDistretto].shown = false
		} else if n, err := strconv.Atoi(st.out[LocMunicipalita].value); err == nil {
			if d, ok := napoli.Distretto(n); ok {
				st.out[LocDistretto].value, st.out[LocDistretto].source = d, "computed"
			}
		}
	}
	// Notes say only what the caller may see: a stored row it can't see is not
	// compared with the computed value, nor said to be old.
	for i := range st.out {
		if st.out[i].stale && st.out[i].shown {
			since := "the address"
			if i == LocDistretto {
				since = "the address or the stored municipalità"
			}
			st.notes = append(st.notes, "the stored "+locatedLabels[i]+" is older than "+since+" and was ignored")
		}
	}
	if differs != "" && st.out[LocMunicipalita].shown {
		st.notes = append(st.notes, differs)
	}
	return st, nil
}

// usesStored reports that the stored row answers field i as it is.
func (st *locatedState) usesStored(i int) bool { return st.out[i].source == "stored" }

// notResolved is the error for a field with no value.
func (st *locatedState) notResolved() error {
	reason := st.reason
	if reason == "" {
		reason = "no value stored, and the address does not give one"
	}
	return &NotResolvedError{Reason: reason, Choices: st.choices()}
}

func (st *locatedState) choices() []string {
	if st.ran && st.res.Status == napoli.Ambiguous {
		return st.res.QuartiereChoices()
	}
	return nil
}

// info is field i as a computed FieldInfo.
func (st *locatedState) info(i int) FieldInfo {
	_, name, _ := strings.Cut(st.out[i].id, ".")
	return FieldInfo{
		ID: st.out[i].id, Category: st.category, FieldName: name,
		Value: st.out[i].value, Sensitivity: st.out[i].tier,
		UpdatedAt: st.updated, Derived: true,
	}
}

// getLocated answers a read of one computed field: a stored row that still
// stands as any stored field, else the computed value.
func (v *Vault) getLocated(id string, k locatedKind, category string, which int, allow FieldFilter) (*FieldInfo, error) {
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}
	defer clear(vaultKey)
	st, err := v.locate(vaultKey, category, k, allow)
	if err != nil {
		return nil, err
	}
	switch {
	case st.usesStored(which):
		return v.getStored(id, allow)
	case !st.out[which].shown:
		return nil, nil
	case st.out[which].value == "":
		return nil, st.notResolved()
	}
	v.db.LogAccess(auditRead(id))
	info := st.info(which)
	return &info, nil
}

// overlayLocated returns the fields of an address's category with its three
// computed fields as the caller sees them: a stored row that no longer stands
// is dropped, and a value the vault computes is added.
func (v *Vault) overlayLocated(vaultKey []byte, category string, fields []FieldInfo, allow FieldFilter) ([]FieldInfo, error) {
	k, ok := locatedKindOfCategory(category)
	if !ok {
		return fields, nil
	}
	st, err := v.locate(vaultKey, category, k, allow)
	if err != nil {
		return nil, err
	}
	out := make([]FieldInfo, 0, len(fields)+3)
	for _, f := range fields {
		if _, _, which, isOutput := locatedOutput(f.ID); isOutput && !st.usesStored(which) {
			continue
		}
		out = append(out, f)
	}
	for i := range st.out {
		if !st.usesStored(i) && st.out[i].shown && st.out[i].value != "" {
			out = append(out, st.info(i))
		}
	}
	return out, nil
}

// Locate says where an address of the vault is, as far as allow lets the
// caller see: the computed (or stored) quartiere, municipalità and distretto,
// what settled them, and, when they can't be known, why and what to ask.
func (v *Vault) Locate(category string, allow FieldFilter) (*Located, error) {
	k, ok := locatedKindOfCategory(category)
	if !ok {
		return nil, ErrNotLocatable
	}
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}
	defer clear(vaultKey)
	st, err := v.locate(vaultKey, category, k, allow)
	if err != nil {
		return nil, err
	}
	res := &Located{Category: category, Fields: map[string]LocatedField{}, Notes: st.notes}
	for i := range st.out {
		if st.out[i].shown && st.out[i].value != "" {
			res.Fields[st.out[i].id] = LocatedField{Value: st.out[i].value, Source: st.out[i].source}
			v.db.LogAccess(auditRead(st.out[i].id))
		}
	}
	if m := st.out[LocMunicipalita]; m.shown && m.source == "computed" {
		res.Basis, res.Partial = string(st.res.Basis), st.res.Partial
	}
	if m := st.out[LocMunicipalita]; m.shown && m.value == "" {
		res.Reason, res.Choices = st.reason, st.choices()
	}
	return res, nil
}

// overlayContext adds the computed fields of every address in the vault to a
// context bundle. The bundle holds only what allow accepted, but an address is
// located from all its rows, so a token scoped to residenza.municipalita
// still gets it.
func (v *Vault) overlayContext(bundle *ContextBundle, allow FieldFilter) error {
	rows, err := v.db.ListFields()
	if err != nil {
		return err
	}
	var categories []string
	for _, r := range rows {
		if _, ok := locatedKindOfCategory(r.Category); ok && !slices.Contains(categories, r.Category) {
			categories = append(categories, r.Category)
		}
	}
	if len(categories) == 0 {
		return nil
	}
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return err
	}
	defer clear(vaultKey)
	for _, c := range categories {
		fields, err := v.overlayLocated(vaultKey, c, bundle.Categories[c], allow)
		if err != nil {
			return err
		}
		if len(fields) == 0 {
			delete(bundle.Categories, c)
		} else {
			bundle.Categories[c] = fields
		}
	}
	return nil
}

// Rules for what may be stored in the computed fields.

var (
	// A municipalità as people write it: "3", "III", "3ª", "Municipalità 3",
	// "Municipalità n. 3", "mun. 3", "III Municipalità".
	reMunicipalita = regexp.MustCompile(`^(?:(?:MUNICIPALIT[AÀ]|MUN)['’]?\.?\s*)?(?:N[°º.]*\s*)?([0-9]{1,2}|[IVX]{1,4})\s*[ªº°^.]?\s*(?:MUNICIPALIT[AÀ]['’]?)?$`)
	// A distretto: "31", "24/73", "DS 31", "ds31", "D.S. 25", "DS-27",
	// "Distretto sanitario n. 33".
	reDistretto = regexp.MustCompile(`^(?:DISTRETTO\s+)?(?:SANITARIO\s+)?(?:D\.?\s*S\.?)?[\s:-]*(?:N[°º.]*)?\s*([0-9]{1,2}(?:\s*/\s*[0-9]{1,2})?)$`)

	romanNumerals = map[string]int{"I": 1, "II": 2, "III": 3, "IV": 4, "V": 5, "VI": 6, "VII": 7, "VIII": 8, "IX": 9, "X": 10}
)

// normalizeLocated applies the rules of a stored quartiere, municipalità or
// distretto. handled is false for any other field.
func normalizeLocated(id, category, name, value string) (out string, handled bool, err error) {
	which := -1
	for _, c := range locatedKinds {
		if i := slices.Index(c.outputs(), name); c.category == category && i >= 0 {
			which = i
		}
	}
	if which < 0 {
		return value, false, nil
	}
	fail := func(msg string) (string, bool, error) {
		return "", true, &ValidationError{Field: id, Msg: msg}
	}
	s := strings.ToUpper(strings.TrimSpace(value))
	switch which {
	case LocQuartiere:
		if q, ok := napoli.FindQuartiere(value); ok {
			return q.Name, true, nil
		}
		names := make([]string, 0, 30)
		for _, q := range napoli.Quartieri() {
			names = append(names, q.Name)
		}
		return fail("must be one of the quartieri of Napoli: " + strings.Join(names, ", "))
	case LocMunicipalita:
		if m := reMunicipalita.FindStringSubmatch(s); m != nil {
			if n, ok := romanNumerals[m[1]]; ok {
				return strconv.Itoa(n), true, nil
			}
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= 10 {
				return strconv.Itoa(n), true, nil
			}
		}
		return fail("must be a municipalità of Napoli, 1 to 10 (or I to X)")
	default:
		if m := reDistretto.FindStringSubmatch(s); m != nil {
			s := strings.ReplaceAll(m[1], " ", "")
			for mun := 1; mun <= 10; mun++ {
				d, _ := napoli.Distretto(mun)
				if s == d || slices.Contains(strings.Split(d, "/"), s) {
					return d, true, nil
				}
			}
		}
		return fail("must be a distretto sanitario of " + napoli.ASL + ": 24/73 or 25 to 33")
	}
}
