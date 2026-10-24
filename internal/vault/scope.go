package vault

import (
	"fmt"
	"regexp"
	"strings"
)

// validIDPart matches alphanumeric, underscores, and hyphens.
var validIDPart = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateFieldID checks that a field ID is safe: category.field_name where both
// parts contain only alphanumeric characters, underscores, and hyphens.
func ValidateFieldID(id string) error {
	parts := strings.SplitN(id, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("field ID must be category.field_name, got %q", id)
	}
	if !validIDPart.MatchString(parts[0]) {
		return fmt.Errorf("invalid category %q: only alphanumeric, underscore, hyphen allowed", parts[0])
	}
	if !validIDPart.MatchString(parts[1]) {
		return fmt.Errorf("invalid field name %q: only alphanumeric, underscore, hyphen allowed", parts[1])
	}
	return nil
}

// ValidCategoryName checks that a category name contains only safe characters.
func ValidCategoryName(name string) bool {
	return name != "" && validIDPart.MatchString(name)
}

// ScopeAllows checks if a comma-separated scope pattern allows access to a field ID.
// Patterns: "*" (all), "identity.*" (category), "identity.full_name" (exact).
func ScopeAllows(scope, fieldID string) bool {
	for _, p := range strings.Split(scope, ",") {
		p = strings.TrimSpace(p)
		if p == "*" {
			return true
		}
		if strings.HasSuffix(p, ".*") {
			category := strings.TrimSuffix(p, ".*")
			if strings.HasPrefix(fieldID, category+".") {
				return true
			}
			continue
		}
		if p == fieldID {
			return true
		}
	}
	return false
}

// ScopeAllowsCategory checks if a scope pattern allows access to any field in a category.
func ScopeAllowsCategory(scope, category string) bool {
	for _, p := range strings.Split(scope, ",") {
		p = strings.TrimSpace(p)
		if p == "*" {
			return true
		}
		if strings.HasSuffix(p, ".*") {
			if strings.TrimSuffix(p, ".*") == category {
				return true
			}
			continue
		}
		// Exact field pattern — allows category if the field is in it
		if strings.HasPrefix(p, category+".") {
			return true
		}
	}
	return false
}

// ValidateScope checks that every comma-separated pattern in a token scope is
// "*", "category.*", or an exact field ID. Empty scopes are rejected so that
// full access always has to be requested explicitly.
func ValidateScope(scope string) error {
	if strings.TrimSpace(scope) == "" {
		return fmt.Errorf("scope is required (e.g. \"identity.*,residenza.*\", or \"*\" for full access)")
	}
	for _, p := range strings.Split(scope, ",") {
		p = strings.TrimSpace(p)
		switch {
		case p == "*":
		case strings.HasSuffix(p, ".*"):
			if !ValidCategoryName(strings.TrimSuffix(p, ".*")) {
				return fmt.Errorf("invalid scope pattern %q", p)
			}
		default:
			if err := ValidateFieldID(p); err != nil {
				return fmt.Errorf("invalid scope pattern %q: %w", p, err)
			}
		}
	}
	return nil
}
