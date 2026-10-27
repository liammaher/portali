package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// maxGetFields matches the Touch ID helper's limit on scope patterns
// (maxScopePatterns in cmd/pvault-touchid/main.swift), so one prompt can
// always cover every field and category asked for.
const maxGetFields = 10

func cmdGet() {
	if len(os.Args) < 3 {
		fatal("usage: pvault get <id | category.*> [...]\n  example: pvault get identity.full_name\n  several (one approval, JSON output): pvault get identity.full_name 'residenza.*' 'leo:identity.*'\n  a category.* leaves out personal fields (PINs, passwords): name one to read it")
	}
	args, err := parseGetIDs(os.Args[2:])
	if err != nil {
		fatal("%v", err)
	}
	// The output format follows what was typed, not what's left after
	// de-duplication, so a script passing several IDs always gets JSON.
	many := len(os.Args) > 3 || slices.ContainsFunc(args, isCategoryPattern)
	ids, values, err := getFields(args, many)
	if err != nil {
		fatal("%v", err)
	}
	if !many {
		fmt.Println(*values[ids[0]])
		return
	}
	fmt.Println(formatGetJSON(ids, values))
}

// parseGetIDs validates and de-duplicates the requested field IDs and
// category patterns before anything is asked of the user, so the prompt
// names only well-formed fields.
func parseGetIDs(args []string) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, id := range args {
		if !isCategoryPattern(id) {
			if err := vault.ValidateFieldID(id); err != nil {
				return nil, err
			}
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > maxGetFields {
		return nil, fmt.Errorf("at most %d fields or categories per get (identity.* reads a whole category)", maxGetFields)
	}
	return ids, nil
}

// isCategoryPattern reports a whole category: identity.*, one person's
// (leo:identity.*) or everyone's (*:identity.*), as a scope names it.
func isCategoryPattern(arg string) bool {
	category, ok := strings.CutSuffix(arg, ".*")
	if !ok {
		return false
	}
	if rest, everyone := strings.CutPrefix(category, "*:"); everyone {
		return !strings.Contains(rest, ":") && vault.ValidCategoryName(rest)
	}
	return vault.ValidCategoryName(category)
}

// getFields reads every field asked for with one grant scoped to exactly
// those fields and categories. It returns the IDs read, in the order asked,
// a category's fields sorted in its place. A category leaves out personal
// fields: a form never needs a PIN or password, so one has to be named to
// be read. With many set, a field asked for by ID that doesn't exist comes
// back nil instead of failing the others; otherwise it is an error.
func getFields(args []string, many bool) ([]string, map[string]*string, error) {
	needGrant(strings.Join(args, ","), "read")
	var listed []vault.FieldInfo
	if slices.ContainsFunc(args, isCategoryPattern) {
		resp, err := apiRequest("GET", "/vault/fields", nil)
		if err != nil {
			return nil, nil, fmt.Errorf("request failed: %v", err)
		}
		if err := apiResult(resp, &listed); err != nil {
			return nil, nil, err
		}
		slices.SortFunc(listed, func(a, b vault.FieldInfo) int { return strings.Compare(a.ID, b.ID) })
	}

	var ids []string
	asked := map[string]bool{} // by ID: a missing one is null, not left out
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, arg := range args {
		if !isCategoryPattern(arg) {
			asked[arg] = true
			add(arg)
			continue
		}
		start := len(ids)
		for _, f := range listed {
			if f.Sensitivity != vault.TierPersonal && vault.ScopeAllows(arg, f.ID) {
				add(f.ID)
			}
		}
		// The fields the vault computes for an address (its municipalità) are
		// read like stored ones, and left out when it can't work them out.
		if category, _ := strings.CutSuffix(arg, ".*"); !strings.HasPrefix(category, "*:") {
			if _, computed, ok := vault.LocatedTarget(category); ok {
				for _, id := range computed {
					add(id)
				}
			}
		}
		slices.Sort(ids[start:])
	}

	values := make(map[string]*string, len(ids))
	kept := ids[:0]
	for _, id := range ids {
		resp, err := apiRequest("GET", "/vault/fields/"+id, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusNotFound && (many || !asked[id]) {
			resp.Body.Close()
			if asked[id] {
				values[id] = nil
				kept = append(kept, id)
			}
			continue // deleted since it was listed
		}
		var field vault.FieldInfo
		if err := apiResult(resp, &field); err != nil {
			return nil, nil, fmt.Errorf("%s: %v", id, err)
		}
		values[id] = &field.Value
		kept = append(kept, id)
	}
	return kept, values, nil
}

// formatGetJSON prints the values as a JSON object in the order asked, with
// null for fields that don't exist.
func formatGetJSON(ids []string, values map[string]*string) string {
	var b strings.Builder
	b.WriteString("{\n")
	for i, id := range ids {
		fmt.Fprintf(&b, "  %s: %s", jsonText(id), jsonText(values[id]))
		if i < len(ids)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String()
}

// jsonText encodes v without json.Marshal's HTML escaping, so a value like
// "Via Roma 1 & 2" prints as typed rather than with \u0026.
func jsonText(v any) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return strings.TrimSuffix(buf.String(), "\n")
}
