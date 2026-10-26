package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// GET /vault/members
// Lists family members, limited like every bulk read to the caller's scope
// and tier cap: a member shows only if the caller can reach some of their
// data, and derived values (age, label) only from fields it can read.
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	h, err := s.vault.Household(callerFilter(r), time.Now())
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// DELETE /vault/members/{slug}
// Deletes every field and document of a member and revokes the tokens and
// grants that name them. Only the user may: their session, or a grant they
// approved in person (Touch ID) for write access to everything about that
// member, whose prompt names exactly that. Service tokens never may.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	approved := reachesPersonal(r) && accessFromRequest(r) == vault.AccessWrite &&
		vault.ValidMemberSlug(slug) && vault.ScopeAllowsWholeMember(scopeFromRequest(r), slug)
	if !isSessionAuth(r) && !approved {
		sessionRequired(w)
		return
	}
	out, err := s.vault.RemoveMember(slug)
	var inUse *vault.MemberInUseError
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
