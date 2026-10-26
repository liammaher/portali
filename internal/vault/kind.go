package vault

import (
	"fmt"
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
//     secrets (CIE PIN, firma digitale PIN): a business may never hold them.
func (v *Vault) checkKind(id string) error {
	person, local := SplitPerson(id)
	category, _, _ := strings.Cut(local, ".")
	if err := v.checkKindCategory(person, category, id); err != nil {
		return err
	}
	if f := GetSchemaField(strings.ToLower(local)); f != nil && len(f.Kinds) > 0 && !slices.Contains(f.Kinds, v.kind) {
		return &ValidationError{Field: id, Msg: "is only for " + strings.Join(f.Kinds, "/") + " vaults; this one is " + v.kind}
	}
	// The whole SPID/CieID account, not just its secrets: IsCredentialField
	// lets spid.provider through, which a business has no use for either.
	if v.kind == KindImpresa && person != "" && (IsCredentialField(id) || IsPersonalCategory(category)) {
		return &ValidationError{Field: id, Msg: errPartyCredential}
	}
	return nil
}

// checkKindDocument is checkKind for a document category ([person:]category).
func (v *Vault) checkKindDocument(category string) error {
	person, base := SplitPerson(category)
	field := DocumentScopeID(category)
	if err := v.checkKindCategory(person, base, field); err != nil {
		return err
	}
	if v.kind == KindImpresa && person != "" && DefaultDocumentTier(category) == TierPersonal {
		return &ValidationError{Field: field, Msg: errPartyCredential}
	}
	return nil
}

const errPartyCredential = "a business vault never holds other people's SPID/CieID accounts or login secrets (PIN, PUK, passwords): each person keeps their own"

// checkKindCategory applies the category rules of checkKind. field names the
// ID in the error.
func (v *Vault) checkKindCategory(person, category, field string) error {
	c := GetSchemaCategory(strings.ToLower(category))
	if c == nil {
		return nil // user-defined categories are allowed anywhere
	}
	if len(c.Kinds) > 0 && !slices.Contains(c.Kinds, v.kind) {
		return &ValidationError{Field: field, Msg: c.Name + " is only for " + strings.Join(c.Kinds, "/") + " vaults; this one is " + v.kind}
	}
	if v.kind == KindImpresa && person == "" && c.Subject == SubjectPersona {
		return &ValidationError{Field: field, Msg: c.Name + " describes a person, and this vault's own data is the business's: store it on the person, e.g. bianchi:" + strings.ToLower(category) + ".…"}
	}
	return nil
}
