package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// GET /vault/members
// Lists family members, limited like every bulk read to the caller's scope
// and tier cap: a member shows only if the caller can reach some of their
// data, and derived values (age, label) only from fields it can read. The
// session and grants the user approved see everyone, people with no fields
// or documents yet too.
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	h, err := s.vault.Household(callerFilter(r), isUser(r), time.Now())
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// DELETE /vault/members/{slug} (the route keeps its name from before the
// rename to "person")
// Deletes every field and document of a person and revokes the tokens and
// grants that name them. Only the user may: their session, or a grant they
// approved in person (Touch ID) for write access to everything about that
// person, whose prompt names exactly that. Service tokens never may.
func (s *Server) handleRemovePerson(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	approved := isUser(r) && accessFromRequest(r) == vault.AccessWrite &&
		vault.ValidPersonSlug(slug) && vault.ScopeAllowsWholePerson(scopeFromRequest(r), slug)
	if !isSessionAuth(r) && !approved {
		sessionRequired(w)
		return
	}
	out, err := s.vault.RemovePerson(slug)
	var inUse *vault.PersonInUseError
	switch {
	case errors.As(err, &inUse):
		writeJSON(w, http.StatusConflict, map[string]any{"error": inUse.Error(), "constraint": "member_in_use", "refs": inUse.Refs})
	case err != nil:
		handleVaultError(w, err)
	default:
		writeJSON(w, http.StatusOK, out)
	}
}

// POST /vault/members/migrate[?dry_run=1]
// Session only: turns nucleo_familiare.membro<N>_* fields into members.
func (s *Server) handleMigrateMembers(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	dryRun := false
	if v := r.URL.Query().Get("dry_run"); v != "" {
		var err error
		if dryRun, err = strconv.ParseBool(v); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "dry_run must be 1/true or 0/false")
			return
		}
	}
	report, err := s.vault.MigrateNucleo(dryRun)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// GET /vault/members/{slug}/export[?include_personal=1]
// Session only: every field and document of one person, documents with
// their content, for their right to data portability. Personal-tier items
// are left out unless include_personal.
func (s *Server) handleExportPerson(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	includePersonal := false
	if v := r.URL.Query().Get("include_personal"); v != "" {
		var err error
		if includePersonal, err = strconv.ParseBool(v); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "include_personal must be 1/true or 0/false")
			return
		}
	}
	out, err := s.vault.ExportPerson(r.PathValue("slug"), includePersonal)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// retentionSettings is the body of /vault/retention: years is 0 when not
// set; scan_days is left out when not set (0 is a period: right away).
type retentionSettings struct {
	Years    *int `json:"years,omitempty"`
	ScanDays *int `json:"scan_days,omitempty"`
}

// GET /vault/retention
// The retention period in years and the period for clients' ID scans.
func (s *Server) handleGetRetention(w http.ResponseWriter, r *http.Request) {
	out, err := s.retentionSettings()
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) retentionSettings() (retentionSettings, error) {
	years, err := s.vault.RetentionYears()
	if err != nil {
		return retentionSettings{}, err
	}
	out := retentionSettings{Years: &years}
	if days, set, err := s.vault.ScanDays(); err != nil {
		return retentionSettings{}, err
	} else if set {
		out.ScanDays = &days
	}
	return out, nil
}

// PUT /vault/retention {"years": N} and/or {"scan_days": N}
// Session only: sets the retention period (years 0 removes it) and the
// period for clients' ID scans (scan_days -1 removes it).
func (s *Server) handleSetRetention(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	if !requireJSON(w, r) {
		return
	}
	var req retentionSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Years == nil && req.ScanDays == nil) {
		writeError(w, http.StatusBadRequest, "invalid_request", `body must be {"years": N} or {"scan_days": N}`)
		return
	}
	if err := s.vault.SetRetention(req.Years, req.ScanDays); err != nil {
		handleVaultError(w, err)
		return
	}
	s.handleGetRetention(w, r)
}
