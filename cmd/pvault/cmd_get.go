package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// maxGetFields matches the Touch ID helper's limit on scope patterns
// (maxScopePatterns in cmd/pvault-touchid/main.swift), so one prompt can
// always cover every field asked for.
const maxGetFields = 10

func cmdGet() {
	if len(os.Args) < 3 {
		fatal("usage: pvault get <id> [<id>...]\n  example: pvault get identity.full_name\n  several fields (one approval, JSON output): pvault get identity.full_name identity.codice_fiscale")
	}
	ids, err := parseGetIDs(os.Args[2:])
	if err != nil {
		fatal("%v", err)
	}
	// The output format follows what was typed, not what's left after
	// de-duplication, so a script passing several IDs always gets JSON.
	many := len(os.Args) > 3
	values, err := getFields(ids, many)
	if err != nil {
		fatal("%v", err)
	}
	if !many {
		fmt.Println(*values[ids[0]])
		return
	}
	fmt.Println(formatGetJSON(ids, values))
}

// parseGetIDs validates and de-duplicates the requested field IDs before
// anything is asked of the user, so the prompt names only well-formed fields.
func parseGetIDs(args []string) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, id := range args {
		if err := vault.ValidateFieldID(id); err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > maxGetFields {
		return nil, fmt.Errorf("at most %d fields per get (use 'pvault list <category>' for a whole category)", maxGetFields)
	}
	return ids, nil
}

// getFields reads every id with one grant scoped to exactly those fields.
// With many set, a field that doesn't exist comes back nil instead of
// failing the others; otherwise it is an error, as before.
func getFields(ids []string, many bool) (map[string]*string, error) {
	needGrant(strings.Join(ids, ","), "read")
	values := make(map[string]*string, len(ids))
	for _, id := range ids {
		resp, err := apiRequest("GET", "/vault/fields/"+id, nil)
		if err != nil {
			return nil, fmt.Errorf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusNotFound && many {
			resp.Body.Close()
			values[id] = nil
			continue
		}
		var field vault.FieldInfo
		if err := apiResult(resp, &field); err != nil {
			return nil, fmt.Errorf("%s: %v", id, err)
		}
		values[id] = &field.Value
	}
	return values, nil
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
// "Via Roma 1 & 2" prints as typed rather than with &.
func jsonText(v any) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return strings.TrimSuffix(buf.String(), "\n")
}
