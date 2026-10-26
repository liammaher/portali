package vault

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// A vault's kind says what its principal (the unprefixed IDs) is: a person
// (the personal vault: you, with your family as other people) or an impresa
// (a business vault: the business, with its titolare, soci, employees and
// clients as other people, each with a ruolo). It is stored in vault_meta at
// init and never changes; vaults from before kinds existed are persona.
// See docs/design/business-vaults.md.
const (
	KindPersona = "persona"
	KindImpresa = "impresa"
)

const metaKind = "kind"

// InitOptions are the choices made when a vault is created.
type InitOptions struct {
	Kind string // KindPersona (the default) or KindImpresa
}

// validKind normalizes a kind; "" is persona.
func validKind(kind string) (string, error) {
	switch kind {
	case "", KindPersona:
		return KindPersona, nil
	case KindImpresa:
		return KindImpresa, nil
	}
	return "", fmt.Errorf("invalid vault kind %q: use %s or %s", kind, KindPersona, KindImpresa)
}

// Kind returns the vault's kind.
func (v *Vault) Kind() string {
	return v.kind
}

// checkKind refuses a field the vault's kind doesn't allow. Like
// checkPersonRefs, it needs the vault, so it isn't part of the pure
// NormalizeAndValidate (whose rules must treat every person the same):
//   - a category the schema keeps for the other kind (famiglia in a business
//     vault, ruolo in a personal one);
//   - in a business vault, a category about a person stored for the business
//     itself (identity.codice_fiscale belongs to someone: bianchi:identity...);
//   - in a business vault, other people's SPID/CieID accounts and login
//     secrets (CIE PIN, Fisconline PIN): a business may never hold them.
//     Its own data may hold its own SPID (per uso professionale) and portal
//     logins (entratel.password), but no CIE, CieID or digital signature
//     secret, which is always a person's, not even under a SPID category
//     (spid.cie_pin).
//
// A document category is checked as its scope ID (DocumentScopeID), so its
// documents follow the same rules.
func (v *Vault) checkKind(id string) error {
	person, local := SplitPerson(id)
	category, _, _ := strings.Cut(local, ".")
	if err := v.checkKindCategory(person, category, id); err != nil {
		return err
	}
	if f := GetSchemaField(canonicalLocalID(local)); f != nil && len(f.Kinds) > 0 && !slices.Contains(f.Kinds, v.kind) {
		return &ValidationError{Field: id, Msg: "is only for " + strings.Join(f.Kinds, "/") + " vaults; this one is " + v.kind}
	}
	// The whole SPID/CieID account, not just its secrets: IsCredentialField
	// lets spid.provider through, which a business has no use for either.
	if v.kind == KindImpresa && person != "" && (IsCredentialField(id) || IsPersonalCategory(category)) {
		return &ValidationError{Field: id, Msg: errPartyCredential}
	}
	if v.kind == KindImpresa && person == "" && (IsIdentityCredential(id) || IsPersonalCategory(category)) &&
		(!isSPIDCategory(category) || namesNonSPIDIdentity(local)) {
		return &ValidationError{Field: id, Msg: errBusinessCredential}
	}
	return nil
}

// CanHold reports whether this vault's own data (person false) or another
// person may hold a field ID: the checks Set makes before reading the value
// (the vault's kind, owner-only and person-only categories and fields). The
// onboarding page shows only what can be saved from these answers.
func (v *Vault) CanHold(id string, person bool) bool {
	if person {
		id = "x:" + id // the rules treat every person alike
	}
	return v.checkKind(id) == nil && checkHolder(id) == nil
}

const (
	errPartyCredential    = "a business vault never holds other people's SPID/CieID accounts or login secrets (PIN, PUK, passwords): each person keeps their own"
	errBusinessCredential = "a business vault holds identity login secrets only for the business's own SPID (spid.*): a CIE, CieID or digital signature is always a person's, and each person keeps their own"
)

// isSPIDCategory reports a SPID category (spid, spid2, spid_poste): the only
// account a business has itself.
func isSPIDCategory(category string) bool {
	words := idWords(strings.ToLower(category))
	return len(words) > 0 && reSPIDWord.MatchString(words[0])
}

var reSPIDWord = regexp.MustCompile(`^spid[0-9]*$`)

// namesNonSPIDIdentity reports a word of a field's category or name that
// names a CIE, CieID or digital signature, so spid.cie_pin or spid_firma.puk
// can't pass as the business's own SPID.
func namesNonSPIDIdentity(local string) bool {
	for _, w := range idWords(strings.ReplaceAll(strings.ToLower(local), ".", "_")) {
		if reIdentityWord.MatchString(w) && !reSPIDWord.MatchString(w) {
			return true
		}
	}
	return false
}

// schemaCategoryOf returns the schema category a category is, or is a
// numbered copy of: cie2 and identity-2 follow the rules of cie and
// identity, as spid2 is a second SPID account.
func schemaCategoryOf(category string) *SchemaCategory {
	name := strings.ToLower(category)
	if c := GetSchemaCategory(name); c != nil {
		return c
	}
	return GetSchemaCategory(strings.TrimRight(strings.TrimRight(name, "0123456789"), "_-"))
}

// canonicalLocalID returns a field ID without its person, lower-case, with a
// numbered copy's category named as the schema category it copies:
// Cie2.Numero gives cie.numero. Every rule keyed by category or schema field
// reads this, so a copy follows exactly the rules of the original.
func canonicalLocalID(local string) string {
	local = strings.ToLower(local)
	category, name, ok := strings.Cut(local, ".")
	if c := schemaCategoryOf(category); c != nil {
		category = c.Name
	}
	if !ok {
		return category
	}
	return category + "." + name
}

// checkKindCategory applies the category rules of checkKind. field names the
// ID in the error.
func (v *Vault) checkKindCategory(person, category, field string) error {
	c := schemaCategoryOf(category)
	if c == nil {
		return nil // user-defined categories are allowed anywhere
	}
	if len(c.Kinds) > 0 && !slices.Contains(c.Kinds, v.kind) {
		return &ValidationError{Field: field, Msg: c.Name + " is only for " + strings.Join(c.Kinds, "/") + " vaults; this one is " + v.kind}
	}
	if v.kind == KindImpresa && IsHousingCategory(category) {
		// Its own home, in a personal vault: a person can't have one either
		// (IsOwnerOnlyCategory), so no spelling would do.
		return &ValidationError{Field: field, Msg: c.Name + " describes the home in a personal vault; a business vault keeps its premises in locale.*"}
	}
	if v.kind == KindImpresa && person == "" && c.Subject == SubjectPersona {
		return &ValidationError{Field: field, Msg: c.Name + " describes a person, and this vault's own data is the business's: store it on the person, e.g. bianchi:" + strings.ToLower(category) + ".…"}
	}
	return nil
}
