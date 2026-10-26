package vault

import (
	"fmt"
	"regexp"
	"strings"
)

// validIDPart matches alphanumeric, underscores, and hyphens.
var validIDPart = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Household members. A member's fields and documents live in the same
// categories as the owner's, with the member's slug in front:
// leo:identity.codice_fiscale, leo:cie (documents). The owner's IDs have no
// prefix. The stored category (leo:cie) is the HKDF info, so each person's
// data has its own keys.

// OwnerRef names the vault's owner where a value refers to a person
// (famiglia.genitori, famiglia.intestatario_scheda, pratiche persona).
const OwnerRef = "io"

// reMemberSlug matches a member's slug: lower-case only, so the stored
// category is always byte-identical to what the user types.
var reMemberSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidMemberSlug reports a well-formed member slug that isn't reserved.
func ValidMemberSlug(slug string) bool {
	return slug != OwnerRef && reMemberSlug.MatchString(slug)
}

// cutMember splits a field ID or category at a ':' that comes before the
// first '.'; prefixed is false when there is none (an owner ID).
func cutMember(id string) (member, local string, prefixed bool) {
	i := strings.IndexByte(id, ':')
	if i < 0 {
		return "", id, false
	}
	if dot := strings.IndexByte(id, '.'); dot >= 0 && dot < i {
		return "", id, false
	}
	return id[:i], id[i+1:], true
}

// SplitMember splits a field ID or category into the member's slug ("" for
// the owner) and the rest: "leo:cie.numero" gives "leo" and "cie.numero".
// It doesn't validate the slug, so rules keyed by category can strip any
// prefix before matching.
func SplitMember(id string) (member, local string) {
	member, local, _ = cutMember(id)
	return member, local
}

// errMemberSlug explains a bad member prefix.
func errMemberSlug(member string) error {
	if member == OwnerRef {
		return fmt.Errorf("%q is reserved for the vault's owner: owner fields have no prefix", OwnerRef)
	}
	return fmt.Errorf("invalid member %q: lower-case letters, digits and hyphens, starting with a letter (at most 32)", member)
}

// ValidateFieldID checks that a field ID is safe: [member:]category.field_name
// where category and field name contain only alphanumeric characters,
// underscores, and hyphens, and member is a valid slug.
func ValidateFieldID(id string) error {
	member, local, prefixed := cutMember(id)
	if prefixed && !ValidMemberSlug(member) {
		return errMemberSlug(member)
	}
	parts := strings.SplitN(local, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("field ID must be category.field_name or member:category.field_name, got %q", id)
	}
	if !validIDPart.MatchString(parts[0]) {
		return fmt.Errorf("invalid category %q: only alphanumeric, underscore, hyphen allowed", parts[0])
	}
	if !validIDPart.MatchString(parts[1]) {
		return fmt.Errorf("invalid field name %q: only alphanumeric, underscore, hyphen allowed", parts[1])
	}
	return nil
}

// ValidCategoryName checks that a category name ([member:]category) contains
// only safe characters.
func ValidCategoryName(name string) bool {
	member, local, prefixed := cutMember(name)
	if prefixed && !ValidMemberSlug(member) {
		return false
	}
	return local != "" && validIDPart.MatchString(local)
}

// IsOwnerOnlyCategory reports categories about the home and the owner's own
// procedures, which a member can't have: the housing categories and pratiche.
func IsOwnerOnlyCategory(category string) bool {
	_, category = SplitMember(category)
	return IsHousingCategory(strings.ToLower(category)) || isPraticheCategory(category)
}

// checkMemberCategory refuses an owner-only category with a member prefix.
// field names the ID (or document scope ID) in the error.
func checkMemberCategory(category, field string) error {
	if member, base := SplitMember(category); member != "" && IsOwnerOnlyCategory(base) {
		return &ValidationError{Field: field, Msg: base + " is about the household or your own procedures: store it without a person prefix"}
	}
	return nil
}

// scopePattern is one parsed pattern of a scope.
type scopePattern struct {
	all         bool   // "*": everything, owner and members
	member      string // "" (owner), a slug, or "*" (everyone)
	wholeMember bool   // "leo:*": everything about one member
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
	member, local, prefixed := cutMember(p)
	if prefixed {
		if member != "*" && !ValidMemberSlug(member) {
			return sp, false
		}
		sp.member = member
		if local == "*" {
			// *:* would be "everything" under another name; "*" says it.
			sp.wholeMember = true
			return sp, member != "*"
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

// allowsMember reports a pattern that can reach some of member's data
// ("" is the owner).
func (sp scopePattern) allowsMember(member string) bool {
	return sp.all || sp.member == "*" || sp.member == member
}

// allowsCategory reports a pattern that reaches some of a category.
func (sp scopePattern) allowsCategory(member, category string) bool {
	if !sp.allowsMember(member) {
		return false
	}
	return sp.all || sp.wholeMember || sp.category == category
}

// allows reports a pattern that reaches the field.
func (sp scopePattern) allows(member, category, field string) bool {
	return sp.allowsCategory(member, category) && (sp.all || sp.wholeMember || sp.field == "" || sp.field == field)
}

// scopePatterns parses a comma-separated scope, skipping malformed patterns
// (which ValidateScope refuses when a token or grant is created).
func scopePatterns(scope string) []scopePattern {
	var out []scopePattern
	for _, p := range strings.Split(scope, ",") {
		if sp, ok := parseScopePattern(strings.TrimSpace(p)); ok {
			out = append(out, sp)
		}
	}
	return out
}

// ScopeAllows checks if a comma-separated scope pattern allows access to a field ID.
// Patterns: "*" (all, every person), "identity.*" (an owner category),
// "identity.full_name" (exact), "leo:identity.*" (a member's category),
// "*:identity.*" (that category for the owner and every member), "leo:*"
// (everything about a member). A pattern without a member prefix never
// reaches a member's data.
func ScopeAllows(scope, fieldID string) bool {
	member, local, _ := cutMember(fieldID)
	category, field, _ := strings.Cut(local, ".")
	for _, sp := range scopePatterns(scope) {
		if sp.allows(member, category, field) {
			return true
		}
	}
	return false
}

// ScopeAllowsCategory checks if a scope pattern allows access to any field in
// a category ([member:]category).
func ScopeAllowsCategory(scope, category string) bool {
	member, local, _ := cutMember(category)
	for _, sp := range scopePatterns(scope) {
		if sp.allowsCategory(member, local) {
			return true
		}
	}
	return false
}

// ScopeAllowsMember checks if a scope reaches any data of a member ("" is
// the owner).
func ScopeAllowsMember(scope, member string) bool {
	for _, sp := range scopePatterns(scope) {
		if sp.allowsMember(member) {
			return true
		}
	}
	return false
}

// ScopeNamesMember reports a scope with a pattern naming the member's slug
// (not "*" or "*:..."), i.e. one that was granted for that person.
func ScopeNamesMember(scope, member string) bool {
	for _, sp := range scopePatterns(scope) {
		if !sp.all && sp.member == member {
			return true
		}
	}
	return false
}

// ValidateScope checks that every comma-separated pattern in a token scope is
// "*", "[P:]category.*", "[P:]category.field" or "slug:*" (P a member slug
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
			return fmt.Errorf("invalid scope pattern %q: use \"*\", \"category.*\", \"category.field\", \"member:category.*\", \"*:category.*\" or \"member:*\"", p)
		}
	}
	return nil
}
