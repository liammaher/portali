package api

import (
	"errors"
	"net/http"
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
// Session only: deletes every field and document of a member and revokes
// the tokens and grants that name them.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	out, err := s.vault.RemoveMember(r.PathValue("slug"))
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
	dryRun := r.URL.Query().Get("dry_run") == "1"
	report, err := s.vault.MigrateNucleo(dryRun)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
