package vault

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// The wording of an approval prompt (Touch ID, Windows Hello): what the user
// reads before a fingerprint or face lets a grant through. The Swift helper
// (cmd/pvault-touchid) words its Touch ID prompts the same way; both are held
// to testdata/prompts.json. A prompt is built only from a request the vault
// validated, or the vault's own summary of a signing request, and never
// understates what the grant allows.

const (
	// maxPromptPatterns and maxPromptPatternLength keep every pattern of a
	// scope visible in the prompt.
	maxPromptPatterns      = 10
	maxPromptPatternLength = 100
	maxShownNameLength     = 50
)

// promptScope splits a grant scope into the patterns a prompt names. It is
// stricter than ValidateScope, so a scope it accepts is granted as worded:
// at most maxPromptPatterns patterns of at most maxPromptPatternLength bytes,
// and only spaces and tabs are trimmed, so a line break never reaches it.
func promptScope(scope string) ([]string, error) {
	// Empty pieces stay, so "identity.*," is refused as ValidateScope refuses it.
	parts := strings.Split(scope, ",")
	if len(parts) > maxPromptPatterns {
		return nil, fmt.Errorf("scope must list 1 to %d patterns", maxPromptPatterns)
	}
	for i, p := range parts {
		p = strings.TrimFunc(p, func(r rune) bool { return r == '\t' || unicode.Is(unicode.Zs, r) })
		if _, ok := parseScopePattern(p); !ok || len(p) > maxPromptPatternLength {
			return nil, fmt.Errorf("invalid scope pattern %q", p)
		}
		parts[i] = p
	}
	return parts, nil
}

// PromptVaultLabel names the vault in prompts: "your vault", or "your studio
// vault" for a folder .pvault-studio (pvault --vault studio), so with several
// vaults an approval is never given for the wrong one.
func PromptVaultLabel(dir string) string {
	if name, ok := strings.CutPrefix(filepath.Base(dir), ".pvault-"); ok && ValidPersonSlug(name) {
		return "your " + name + " vault"
	}
	return "your vault"
}

// GrantPrompt completes "pvault wants to …" for a grant of scope with access
// (read if empty), lasting ttl; an empty scope (and access) is full access.
// A scope or access it can't word is an error, never a vaguer prompt.
func GrantPrompt(label, scope, access string, ttl time.Duration) (string, error) {
	if scope == "" {
		if access != "" {
			return "", fmt.Errorf("access needs a scope")
		}
		return fmt.Sprintf("get full access to %s, including settings and agent tokens, for %s", label, promptDuration(ttl)), nil
	}
	patterns, err := promptScope(scope)
	if err != nil {
		return "", err
	}
	// Every scoped grant can read what it covers, append and write included.
	var verb string
	switch access {
	case "", AccessRead:
		verb = "read"
	case AccessAppend:
		verb = "read and add new entries to"
	case AccessWrite:
		verb = "read, change and delete"
	default:
		return "", fmt.Errorf("access must be read, append or write")
	}
	// "*" covers everything, so it is named alone rather than last in a list.
	var what, place string
	for _, p := range patterns {
		if p == "*" {
			what = describePattern(label, "*")
		}
	}
	if what == "" {
		names := make([]string, len(patterns))
		for i, p := range patterns {
			names[i] = describePattern(label, p)
		}
		what = strings.Join(names, ", ")
		place = "in " + label + " "
		if verb == "read" {
			place = "from " + label + " "
		}
	}
	return fmt.Sprintf("%s %s %sfor %s", verb, what, place, promptDuration(ttl)), nil
}

// SignPrompt completes "pvault wants to …" for reviewing a signing request,
// from the vault's own summary of it. The grant it unlocks reads the signed
// file and approves it, so the wording says sign, not read.
func SignPrompt(label, name string, pages int, hash8, requester string, ttl time.Duration) string {
	count := fmt.Sprintf("%d pages", pages)
	if pages == 1 {
		count = "1 page"
	}
	return fmt.Sprintf(`review and sign "%s" (%s, hash %s), filed by %s, with your handwritten signature from %s, for %s`,
		shownName(name), count, hash8, shownName(requester), label, promptDuration(ttl))
}

// shownName keeps what an agent chose as a name harmless in a prompt:
// letters, marks, digits, spaces and . _ - ( ); anything else (quotes, bidi
// controls, line breaks) shows as ?, and a long name is cut. Length counts
// runes, not characters: a character can stack any number of accents, which
// would push the rest of the prompt out of sight.
func shownName(name string) string {
	var out []rune
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || strings.ContainsRune(" ._-()", r) {
			out = append(out, r)
		} else {
			out = append(out, '?')
		}
	}
	for len(out) > 0 && out[0] == ' ' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	if len(out) > maxShownNameLength {
		out = append(out[:maxShownNameLength-1], '…')
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func describePattern(label, pattern string) string {
	if pattern == "*" {
		if label == "your vault" {
			return "everything in your vault, your family's data included"
		}
		return "everything in " + label + ", everyone's data in it included"
	}
	// Whose data: yours (no prefix), one person's, or everyone's.
	whose, all, rest := "your", "all your", pattern
	if person, local, ok := strings.Cut(pattern, ":"); ok {
		rest = local
		if rest == "*" {
			return "everything about " + person
		}
		whose, all = person+"'s", "all of "+person+"'s"
		if person == "*" {
			whose, all = "everyone's", "everyone's"
		}
	}
	// A category pattern also covers <category>.documents (ScopeAllows).
	if category, ok := strings.CutSuffix(rest, ".*"); ok {
		return all + " " + category + " fields and documents"
	}
	if category, ok := strings.CutSuffix(rest, ".documents"); ok {
		return whose + " " + category + " documents"
	}
	if whose == "your" {
		return pattern
	}
	return whose + " " + rest
}

func promptDuration(d time.Duration) string {
	s := int((d + time.Second - 1) / time.Second)
	switch {
	case s == 1:
		return "1 second"
	case s < 60:
		return fmt.Sprintf("%d seconds", s)
	}
	m := (s + 59) / 60 // round up: never understate how long a grant lasts
	if m == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", m)
}
