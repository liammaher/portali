package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
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
	docs, err := s.vault.ListDocuments(callerFilter(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if category := r.URL.Query().Get("category"); category != "" {
		filtered := docs[:0]
		for _, d := range docs {
			if d.Category == category {
				filtered = append(filtered, d)
			}
		}
		docs = filtered
	}
	writeJSON(w, http.StatusOK, docs)
}

// GET /vault/documents/{id}
// Always served as an attachment in a sandbox so an uploaded HTML or SVG file
// cannot run script on the vault's origin.
func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.requireDocumentAccess(w, r, id) {
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
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(info.Name))
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("X-Document-Category", info.Category)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// DELETE /vault/documents/{id}
func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.requireDocumentAccess(w, r, id) {
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
func (s *Server) requireDocumentAccess(w http.ResponseWriter, r *http.Request, id string) bool {
	meta, err := s.vault.DocumentMeta(id)
	if err != nil {
		handleVaultError(w, err)
		return false
	}
	if meta == nil {
		writeError(w, http.StatusNotFound, "not_found", "document not found")
		return false
	}
	if !vault.ScopeAllows(scopeFromRequest(r), vault.DocumentScopeID(meta.Category)) {
		scopeDenied(w)
		return false
	}
	if !vault.TierAllows(maxTierFromRequest(r), meta.Sensitivity) {
		sensitivityDenied(w)
		return false
	}
	return true
}
