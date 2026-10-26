package vault

import (
	"fmt"
	"regexp"
	"strings"
)

// validIDPart matches alphanumeric, underscores, and hyphens.
var validIDPart = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// People. Besides its owner, a vault can hold other people: family members
// in the personal vault (famiglia.*). A person's fields and documents live in
// the same categories as the owner's, with the person's slug in front:
// leo:identity.codice_fiscale, leo:cie (documents). The owner's IDs have no
// prefix. The stored category (leo:cie) is the HKDF info, so each person's
// data has its own keys.

// OwnerRef names the vault's owner where a value refers to a person
// (famiglia.genitori, famiglia.intestatario_scheda, pratiche persona).
const OwnerRef = "io"

// rePersonSlug matches a person's slug: lower-case only, so the stored
// category is always byte-identical to what the user types.
var rePersonSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidPersonSlug reports a well-formed person slug that isn't reserved.
func ValidPersonSlug(slug string) bool {
	return slug != OwnerRef && rePersonSlug.MatchString(slug)
}

// cutPerson splits a field ID or category at a ':' that comes before the
// first '.'; prefixed is false when there is none (an owner ID).
func cutPerson(id string) (person, local string, prefixed bool) {
	i := strings.IndexByte(id, ':')
	if i < 0 {
		return "", id, false
	}
	if dot := strings.IndexByte(id, '.'); dot >= 0 && dot < i {
		return "", id, false
	}
	return id[:i], id[i+1:], true
}

// SplitPerson splits a field ID or category into the person's slug ("" for
// the owner) and the rest: "leo:cie.numero" gives "leo" and "cie.numero".
// It doesn't validate the slug, so rules keyed by category can strip any
// prefix before matching.
func SplitPerson(id string) (person, local string) {
	person, local, _ = cutPerson(id)
	return person, local
}

// errPersonSlug explains a bad person prefix.
func errPersonSlug(person string) error {
	if person == OwnerRef {
		return fmt.Errorf("%q is reserved for the vault's owner: owner fields have no prefix", OwnerRef)
	}
	return fmt.Errorf("invalid person %q: lower-case letters, digits and hyphens, starting with a letter (at most 32)", person)
}

// CheckLowerCase refuses a new field ID or document category with upper-case
// letters. The rules read IDs ignoring case, but the store doesn't, so
// pratiche.x_stato and Pratiche.x_stato would be two rows saying different
// things about one pratica. Rows stored before this rule can still be read,
// re-tiered and deleted.
func CheckLowerCase(id string) error {
	if lower := strings.ToLower(id); id != lower {
		return &ValidationError{Field: id, Msg: "IDs are lower-case: use " + lower}
	}
	return nil
}

// ValidateFieldID checks that a field ID is safe: [person:]category.field_name
// where category and field name contain only alphanumeric characters,
// underscores, and hyphens, and person is a valid slug.
func ValidateFieldID(id string) error {
	person, local, prefixed := cutPerson(id)
	if prefixed && !ValidPersonSlug(person) {
		return errPersonSlug(person)
	}
	parts := strings.SplitN(local, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("field ID must be category.field_name or person:category.field_name, got %q", id)
	}
	if !validIDPart.MatchString(parts[0]) {
		return fmt.Errorf("invalid category %q: only alphanumeric, underscore, hyphen allowed", parts[0])
	}
	if !validIDPart.MatchString(parts[1]) {
		return fmt.Errorf("invalid field name %q: only alphanumeric, underscore, hyphen allowed", parts[1])
	}
	return nil
}

// ValidCategoryName checks that a category name ([person:]category) contains
// only safe characters.
func ValidCategoryName(name string) bool {
	person, local, prefixed := cutPerson(name)
	if prefixed && !ValidPersonSlug(person) {
		return false
	}
	return local != "" && validIDPart.MatchString(local)
}

// IsOwnerOnlyCategory reports categories about the home and the owner's own
// procedures, which a person can't have: the housing categories and pratiche.
func IsOwnerOnlyCategory(category string) bool {
	_, category = SplitPerson(category)
	return IsHousingCategory(strings.ToLower(category)) || isPraticheCategory(category)
}

// checkPersonCategory refuses an owner-only category with a person prefix,
// and a person-only one (procura) without. field names the ID (or document
// scope ID) in the error.
func checkPersonCategory(category, field string) error {
	person, base := SplitPerson(category)
	switch {
	case person != "" && IsOwnerOnlyCategory(base):
		return &ValidationError{Field: field, Msg: base + " is about the household or your own procedures: store it without a person prefix"}
	case person == "" && isPersonOnlyCategory(base):
		return &ValidationError{Field: field, Msg: errPersonOnlyCategory(base)}
	}
	return nil
}

// checkFieldFor refuses a field the schema keeps for the owner (ForOwner)
// with a person prefix, or one it keeps for other people (ForPerson)
// without one.
func checkFieldFor(id string) error {
	person, local := SplitPerson(id)
	f := GetSchemaField(strings.ToLower(local))
	switch {
	case f == nil:
	case f.For == ForOwner && person != "":
		return &ValidationError{Field: id, Msg: "is about you, the vault's owner: store it as " + f.ID + " without a person prefix"}
	case f.For == ForPerson && person == "":
		return &ValidationError{Field: id, Msg: "describes another person in the vault: store it as <person>:" + f.ID + " (e.g. leo:" + f.ID + ")"}
	}
	return nil
}

// scopePattern is one parsed pattern of a scope.
type scopePattern struct {
	all         bool   // "*": everything, owner and people
	person      string // "" (owner), a slug, or "*" (everyone)
	wholePerson bool   // "leo:*": everything about one person
	category    string
	field       string // "" for category.*
}

// parseScopePattern parses one pattern: "*", "[P:]category.*",
// "[P:]category.field" or "slug:*", where P is a slug or "*".
func parseScopePattern(p string) (scopePattern, bool) {
	if p == "*" {
		return scopePattern{all: true}, true
	}
	var sp scopePattern
	person, local, prefixed := cutPerson(p)
	if prefixed {
		if person != "*" && !ValidPersonSlug(person) {
			return sp, false
		}
		sp.person = person
		if local == "*" {
			// *:* would be "everything" under another name; "*" says it.
			sp.wholePerson = true
			return sp, person != "*"
		}
	}
	category, field, ok := strings.Cut(local, ".")
	if !ok || !validIDPart.MatchString(category) {
		return sp, false
	}
	sp.category = category
	if field != "*" {
		if !validIDPart.MatchString(field) {
			return sp, false
		}
		sp.field = field
	}
	return sp, true
}

// allowsPerson reports a pattern that can reach some of a person's data
// ("" is the owner).
func (sp scopePattern) allowsPerson(person string) bool {
	return sp.all || sp.person == "*" || sp.person == person
}

// allowsCategory reports a pattern that reaches some of a category.
func (sp scopePattern) allowsCategory(person, category string) bool {
	if !sp.allowsPerson(person) {
		return false
	}
	return sp.all || sp.wholePerson || sp.category == category
}

// allows reports a pattern that reaches the field.
func (sp scopePattern) allows(person, category, field string) bool {
	return sp.allowsCategory(person, category) && (sp.all || sp.wholePerson || sp.field == "" || sp.field == field)
}

// Scope is a parsed token scope. Parse it once with ParseScope when matching
// many fields (bulk reads); the Scope* functions parse on every call.
type Scope []scopePattern

// ParseScope parses a comma-separated scope, skipping malformed patterns
// (which ValidateScope refuses when a token or grant is created).
func ParseScope(scope string) Scope {
	var out Scope
	for _, p := range strings.Split(scope, ",") {
		if sp, ok := parseScopePattern(strings.TrimSpace(p)); ok {
			out = append(out, sp)
		}
	}
	return out
}

// Allows reports whether the scope reaches a field ID. An ID without a
// category and field (no dot) is reached only by "*".
func (s Scope) Allows(fieldID string) bool {
	person, local, _ := cutPerson(fieldID)
	category, field, ok := strings.Cut(local, ".")
	for _, sp := range s {
		if sp.all || (ok && sp.allows(person, category, field)) {
			return true
		}
	}
	return false
}

// AllowsCategory reports whether the scope reaches any field in a category
// ([person:]category).
func (s Scope) AllowsCategory(category string) bool {
	person, local, _ := cutPerson(category)
	for _, sp := range s {
		if sp.allowsCategory(person, local) {
			return true
		}
	}
	return false
}

// ScopeAllows checks if a comma-separated scope pattern allows access to a field ID.
// Patterns: "*" (all, every person), "identity.*" (an owner category),
// "identity.full_name" (exact), "leo:identity.*" (a person's category),
// "*:identity.*" (that category for the owner and every person), "leo:*"
// (everything about a person). A pattern without a person prefix never
// reaches a person's data.
func ScopeAllows(scope, fieldID string) bool {
	return ParseScope(scope).Allows(fieldID)
}

// ScopeAllowsCategory checks if a scope pattern allows access to any field in
// a category ([person:]category).
func ScopeAllowsCategory(scope, category string) bool {
	return ParseScope(scope).AllowsCategory(category)
}

// ScopeAllowsPerson checks if a scope reaches any data of a person ("" is
// the owner).
func ScopeAllowsPerson(scope, person string) bool {
	for _, sp := range ParseScope(scope) {
		if sp.allowsPerson(person) {
			return true
		}
	}
	return false
}

// ScopeAllowsWholePerson checks if a scope reaches all of a person's data:
// "*" or "slug:*".
func ScopeAllowsWholePerson(scope, person string) bool {
	for _, sp := range ParseScope(scope) {
		if sp.all || (sp.wholePerson && sp.person == person) {
			return true
		}
	}
	return false
}

// ScopeNamesPerson reports a scope with a pattern naming the person's slug
// (not "*" or "*:..."), i.e. one that was granted for that person.
func ScopeNamesPerson(scope, person string) bool {
	for _, sp := range ParseScope(scope) {
		if !sp.all && sp.person == person {
			return true
		}
	}
	return false
}

// ValidateScope checks that every comma-separated pattern in a token scope is
// "*", "[P:]category.*", "[P:]category.field" or "slug:*" (P a person slug
// or "*"). Empty scopes are rejected so that full access always has to be
// requested explicitly.
func ValidateScope(scope string) error {
	if strings.TrimSpace(scope) == "" {
		return fmt.Errorf("scope is required (e.g. \"identity.*,residenza.*\", or \"*\" for full access)")
	}
	for _, p := range strings.Split(scope, ",") {
		p = strings.TrimSpace(p)
		if p == "*:*" {
			return fmt.Errorf("invalid scope pattern %q: use \"*\" for full access", p)
		}
		if _, ok := parseScopePattern(p); !ok {
			return fmt.Errorf("invalid scope pattern %q: use \"*\", \"category.*\", \"category.field\", \"person:category.*\", \"*:category.*\" or \"person:*\"", p)
		}
	}
	return nil
}
