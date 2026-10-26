package api

import (
	_ "embed"
	"net/http"
	"regexp"
	"strings"
)

//go:embed ui/onboarding.html
var onboardingHTML []byte

// pageFields are the field IDs the onboarding page has inputs for.
var pageFields = func() []string {
	var ids []string
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`data-field="([a-z0-9_]+\.[a-z0-9_]+)"`).FindAllSubmatch(onboardingHTML, -1) {
		if id := string(m[1]); !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}()

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(onboardingHTML)
}

// holders says who may hold a field or category: the vault's own data
// (owner) and another person (person).
type holders struct {
	Owner  bool `json:"owner"`
	Person bool `json:"person"`
}

// GET /ui/fields
// For each field on the onboarding page, whether this vault's own data and
// another person may hold it (Vault.CanHold), and for each category whether
// any of its fields can be held, so the page shows only what can be saved
// without copying the vault's rules. It depends only on the vault's kind.
func (s *Server) handleUIFields(w http.ResponseWriter, r *http.Request) {
	fields := map[string]holders{}
	categories := map[string]holders{}
	for _, id := range pageFields {
		h := holders{Owner: s.vault.CanHold(id, false), Person: s.vault.CanHold(id, true)}
		fields[id] = h
		category, _, _ := strings.Cut(id, ".")
		c := categories[category]
		categories[category] = holders{Owner: c.Owner || h.Owner, Person: c.Person || h.Person}
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": fields, "categories": categories})
}
