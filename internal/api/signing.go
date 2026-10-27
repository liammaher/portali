package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// The sign gate's endpoints (docs/design/sign-gate.md). An agent files a
// request; only the user, with a password or fingerprint, reads the stamped
// file and approves it.

type socketKey struct{}

// viaSocket reports whether the request arrived on the vault's Unix socket,
// which only this user's processes can reach.
func viaSocket(r *http.Request) bool {
	ok, _ := r.Context().Value(socketKey{}).(bool)
	return ok
}

// requireSigningScope checks the caller may touch signing requests at all: a
// token's scope has to allow them, as for a document category.
func requireSigningScope(w http.ResponseWriter, r *http.Request) bool {
	if !vault.ScopeAllows(scopeFromRequest(r), vault.SigningScopeID) {
		scopeDenied(w)
		return false
	}
	return true
}

// signingOwner is the requester filter for a caller: the user (a session, or
// a grant they approved) sees every request, an agent only those of its own
// token (two tokens of one consumer name are two agents).
func signingOwner(r *http.Request) string {
	if reachesPersonal(r) {
		return ""
	}
	if id := tokenIDFromRequest(r); id != "" {
		return id
	}
	return "-" // matches no request
}

// requireUser admits the user's own session or the sign grant for request id.
func requireUser(w http.ResponseWriter, r *http.Request, id string) bool {
	if isSessionAuth(r) || (id != "" && signGrantFromRequest(r) == id) {
		return true
	}
	writeError(w, http.StatusForbidden, "session_required", "only you can do this, with your password or fingerprint, not an agent's token")
	return false
}

// POST /vault/signing
func (s *Server) handleSigningRequest(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) || !requireSigningScope(w, r) || !requireAccess(w, r, false) {
		return
	}
	var req struct {
		Name       string            `json:"name"`
		Purpose    string            `json:"purpose"`
		Signer     string            `json:"signer"`
		PDF        []byte            `json:"pdf"`
		Placements []vault.Placement `json:"placements"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the PDF is larger than 10 MB")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON (pdf is base64)")
		return
	}
	info, err := s.vault.RequestSigning(vault.SigningInput{
		Requester: consumerFromRequest(r), RequesterID: signingOwner(r), Name: req.Name, Purpose: req.Purpose,
		Signer: req.Signer, PDF: req.PDF, Placements: req.Placements,
	})
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// GET /vault/signing
func (s *Server) handleSigningList(w http.ResponseWriter, r *http.Request) {
	if !requireSigningScope(w, r) {
		return
	}
	list, err := s.vault.ListSignings(signingOwner(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /vault/signing/{id}
func (s *Server) handleSigningStatus(w http.ResponseWriter, r *http.Request) {
	if !requireSigningScope(w, r) {
		return
	}
	info, err := s.vault.SigningStatus(r.PathValue("id"), signingOwner(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// GET /vault/signing/{id}/summary tells the Touch ID helper what to put in
// its prompt: name, pages and the start of the hash, never the file. Socket
// only.
func (s *Server) handleSigningSummary(w http.ResponseWriter, r *http.Request) {
	if !viaSocket(r) {
		http.NotFound(w, r)
		return
	}
	info, err := s.vault.SigningStatus(r.PathValue("id"), "")
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": info.ID, "name": info.Name, "pages": info.Pages, "signer": info.Signer, "requester": info.Requester,
		"status": info.Status, "date": info.Date, "expires_at": info.ExpiresAt,
		"hash8": info.SHA256[:8],
	})
}

// GET /vault/signing/{id}/candidate
func (s *Server) handleSigningCandidate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !requireUser(w, r, id) {
		return
	}
	info, file, err := s.vault.SigningCandidate(id, consumerFromRequest(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("X-Signing-Sha256", info.SHA256)
	w.Write(file)
}

// POST /vault/signing/{id}/approve
func (s *Server) handleSigningApprove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !requireUser(w, r, id) || !requireJSON(w, r) {
		return
	}
	var req struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SHA256) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "sha256 of the reviewed file is required")
		return
	}
	info, err := s.vault.ApproveSigning(id, req.SHA256, consumerFromRequest(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// DELETE /vault/signing/{id}: the user refuses it, or the agent that asked
// withdraws it.
func (s *Server) handleSigningReject(w http.ResponseWriter, r *http.Request) {
	if !requireSigningScope(w, r) || !requireAccess(w, r, false) {
		return
	}
	info, err := s.vault.RejectSigning(r.PathValue("id"), signingOwner(r), consumerFromRequest(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}
