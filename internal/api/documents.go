package api

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// POST /vault/documents?category=passaporto&name=scan.pdf[&sensitivity=critical]
// The request body is the raw file; Content-Type is the file's type.
func (s *Server) handleAddDocument(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	category := q.Get("category")
	if !vault.ValidCategoryName(category) {
		writeError(w, http.StatusBadRequest, "invalid_request", "category is required (letters, digits, underscore, hyphen)")
		return
	}
	if !vault.ScopeAllows(scopeFromRequest(r), vault.DocumentScopeID(category)) {
		scopeDenied(w)
		return
	}
	// Uploads always create a new document, so append access is enough.
	if !requireAccess(w, r, false) {
		return
	}
	if !reachesPersonal(r) &&
		(q.Get("sensitivity") == vault.TierPersonal || vault.IsPersonalCategory(category)) {
		personalOnlyDenied(w)
		return
	}
	// Only the user's session may store a document below the default tier
	// (as with fields), so an agent can't make it readable by lower caps.
	if sens := q.Get("sensitivity"); sens != "" && !isSessionAuth(r) &&
		sens != vault.DefaultDocumentSensitivity && vault.TierAllows(vault.DefaultDocumentSensitivity, sens) {
		writeError(w, http.StatusForbidden, "session_required", "only your own session can store a document below the default sensitivity tier")
		return
	}

	select {
	case s.uploadSlots <- struct{}{}:
		defer func() { <-s.uploadSlots }()
	default:
		writeError(w, http.StatusTooManyRequests, "busy", "another upload is in progress, try again shortly")
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "document is larger than 20 MB")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read body")
		return
	}

	doc, err := s.vault.AddDocument(category, q.Get("name"), r.Header.Get("Content-Type"), data, q.Get("sensitivity"))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// GET /vault/documents[?category=passaporto]
func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	allow := callerFilter(r)
	if category := r.URL.Query().Get("category"); category != "" {
		scoped := allow
		allow = func(id, sensitivity string) bool {
			return id == vault.DocumentScopeID(category) && scoped(id, sensitivity)
		}
	}
	docs, err := s.vault.ListDocuments(allow)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

// GET /vault/documents/{id}
// Always served as an attachment in a sandbox so an uploaded HTML or SVG file
// cannot run script on the vault's origin.
func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.requireDocumentAccess(w, r, id); !ok {
		return
	}
	info, data, err := s.vault.GetDocument(id)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if info == nil {
		writeError(w, http.StatusNotFound, "not_found", "document not found")
		return
	}
	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": info.Name})
	if disposition == "" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("X-Document-Category", info.Category)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// DELETE /vault/documents/{id}
func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, ok := s.requireDocumentAccess(w, r, id)
	if !ok || !requireAccess(w, r, true) {
		return
	}
	// A re-upload gets the default tier, so deleting one stored above it
	// would let an agent lower it; only the user's session may.
	if !isSessionAuth(r) && !vault.TierAllows(vault.DefaultDocumentSensitivity, meta.Sensitivity) {
		writeError(w, http.StatusForbidden, "session_required", "only your own session can delete a document stored above the default sensitivity tier")
		return
	}
	deleted, err := s.vault.DeleteDocument(id)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "not_found", "document not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// requireDocumentAccess checks the caller's scope and sensitivity cap for a
// document and writes the error response if access is denied or it's missing.
func (s *Server) requireDocumentAccess(w http.ResponseWriter, r *http.Request, id string) (*vault.DocumentInfo, bool) {
	meta, err := s.vault.DocumentMeta(id)
	if err != nil {
		handleVaultError(w, err)
		return nil, false
	}
	// Personal documents don't exist for service tokens.
	if meta == nil || (meta.Sensitivity == vault.TierPersonal && !reachesPersonal(r)) {
		writeError(w, http.StatusNotFound, "not_found", "document not found")
		return nil, false
	}
	if !vault.ScopeAllows(scopeFromRequest(r), vault.DocumentScopeID(meta.Category)) {
		scopeDenied(w)
		return nil, false
	}
	if !vault.TierAllows(maxTierFromRequest(r), meta.Sensitivity) {
		sensitivityDenied(w)
		return nil, false
	}
	return meta, true
}
