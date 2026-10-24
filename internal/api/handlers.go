package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func scopeDenied(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "scope_exceeded", "token scope does not allow access to this field")
}

func sensitivityDenied(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "sensitivity_exceeded", "token is not allowed to access fields at this sensitivity tier")
}

// requireFieldTier checks an existing field's tier against the caller's cap
// and writes the error response if access is denied. Missing fields pass so
// callers can return their usual 404 or create them.
func (s *Server) requireFieldTier(w http.ResponseWriter, r *http.Request, id string) bool {
	maxTier := maxTierFromRequest(r)
	if maxTier == "critical" {
		return true
	}
	meta, err := s.vault.FieldMeta(id)
	if err != nil {
		handleVaultError(w, err)
		return false
	}
	if meta != nil && !vault.TierAllows(maxTier, meta.Sensitivity) {
		sensitivityDenied(w)
		return false
	}
	return true
}

// callerFilter limits bulk reads to fields within the caller's scope and
// sensitivity cap, so nothing else is decrypted.
func callerFilter(r *http.Request) vault.FieldFilter {
	scope, maxTier := scopeFromRequest(r), maxTierFromRequest(r)
	return func(id, sensitivity string) bool {
		return vault.ScopeAllows(scope, id) && vault.TierAllows(maxTier, sensitivity)
	}
}

// requireJSON rejects bodies not sent as application/json. Browsers cannot
// send that cross-origin without a CORS preflight, which this server never
// grants, so web pages cannot hit credential endpoints.
func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_request", "Content-Type must be application/json")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, constraint, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "constraint": constraint})
}

// POST /vault/unlock
func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	if !s.unlockLimit.allow() {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many unlock attempts, try again later")
		return
	}

	var req struct {
		Password  string `json:"password"`
		SecretKey string `json:"secret_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON")
		return
	}
	if req.Password == "" || req.SecretKey == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "password and secret_key required")
		return
	}

	token, err := s.vault.Unlock(req.Password, req.SecretKey)
	if err != nil {
		switch err {
		case vault.ErrWrongPassword:
			writeError(w, http.StatusUnauthorized, "unauthenticated", "wrong password or secret key")
		case vault.ErrAlreadyUnlocked:
			writeError(w, http.StatusConflict, "conflict", "vault is already unlocked")
		case vault.ErrNotInitialized:
			writeError(w, http.StatusPreconditionFailed, "not_initialized", "vault is not initialized")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "internal error")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// POST /vault/session
// Exchanges the password (and optionally the secret key) for a short-lived
// session-level token on an already unlocked vault. The CLI calls this per
// command and the onboarding page on load, instead of storing or passing a
// reusable token. Only failed attempts count toward the rate limit, so normal
// use never locks the user out.
func (s *Server) handleSessionGrant(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	if s.grantLimit.blocked() {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	}

	var req struct {
		Password  string `json:"password"`
		SecretKey string `json:"secret_key"`
		TTL       string `json:"ttl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON")
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "password required")
		return
	}
	ttl := 2 * time.Minute
	if req.TTL != "" {
		parsed, err := time.ParseDuration(req.TTL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid ttl duration")
			return
		}
		ttl = parsed
	}

	token, expires, err := s.vault.CreateSessionGrant(req.Password, req.SecretKey, ttl)
	if err != nil {
		if err == vault.ErrWrongPassword {
			s.grantLimit.recordFailure()
			writeError(w, http.StatusUnauthorized, "unauthenticated", "wrong password or secret key")
			return
		}
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"token":      token,
		"expires_at": expires.UTC().Format(time.RFC3339),
	})
}

// POST /vault/lock
func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	s.vault.Lock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "locked"})
}

// GET /vault/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.vault.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// GET /vault/fields
func (s *Server) handleListFields(w http.ResponseWriter, r *http.Request) {
	fields, err := s.vault.List()
	if err != nil {
		handleVaultError(w, err)
		return
	}
	scope := scopeFromRequest(r)
	allowed := make([]vault.FieldInfo, 0, len(fields))
	for _, f := range fields {
		if vault.ScopeAllows(scope, f.ID) {
			allowed = append(allowed, f)
		}
	}
	writeJSON(w, http.StatusOK, allowed)
}

// GET /vault/fields/{id...}
func (s *Server) handleGetField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := vault.ValidateFieldID(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !vault.ScopeAllows(scopeFromRequest(r), id) {
		scopeDenied(w)
		return
	}
	if !s.requireFieldTier(w, r, id) {
		return
	}
	field, err := s.vault.Get(id)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if field == nil {
		writeError(w, http.StatusNotFound, "not_found", "field not found")
		return
	}
	writeJSON(w, http.StatusOK, field)
}

// PUT /vault/fields/{id...}
func (s *Server) handleSetField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := vault.ValidateFieldID(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !vault.ScopeAllows(scopeFromRequest(r), id) {
		scopeDenied(w)
		return
	}
	if !s.requireFieldTier(w, r, id) {
		return
	}
	var req struct {
		Value       string `json:"value"`
		Sensitivity string `json:"sensitivity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "value required")
		return
	}

	// An empty tier keeps an existing field's tier, or uses the schema
	// default for a new field (see Vault.Set).

	if err := s.vault.Set(id, req.Value, req.Sensitivity); err != nil {
		handleVaultError(w, err)
		return
	}

	resp := map[string]any{"status": "ok"}
	if suggestion := vault.SuggestCanonical(id); suggestion != nil {
		resp["suggestion"] = suggestion
	}
	writeJSON(w, http.StatusOK, resp)
}

// GET /vault/schema
func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, vault.RecommendedSchema)
}

// DELETE /vault/fields/{id...}
func (s *Server) handleDeleteField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := vault.ValidateFieldID(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !vault.ScopeAllows(scopeFromRequest(r), id) {
		scopeDenied(w)
		return
	}
	if !s.requireFieldTier(w, r, id) {
		return
	}
	if err := s.vault.Delete(id); err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GET /vault/fields/category/{category}
func (s *Server) handleGetByCategory(w http.ResponseWriter, r *http.Request) {
	category := r.PathValue("category")
	if !vault.ValidCategoryName(category) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid category name: only alphanumeric, underscore, hyphen allowed")
		return
	}
	scope := scopeFromRequest(r)
	if !vault.ScopeAllowsCategory(scope, category) {
		scopeDenied(w)
		return
	}
	fields, err := s.vault.GetByCategory(category, callerFilter(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fields)
}

// GET /vault/context
func (s *Server) handleGetContext(w http.ResponseWriter, r *http.Request) {
	ctx, err := s.vault.GetContext(callerFilter(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ctx)
}

// GET /vault/audit
func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	entries, err := s.vault.AuditLog(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// PUT /vault/sensitivity/{id...}
// Session only: a service token that could lower a tier could read past its cap.
func (s *Server) handleSetSensitivity(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	id := r.PathValue("id")
	if err := vault.ValidateFieldID(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !vault.ScopeAllows(scopeFromRequest(r), id) {
		scopeDenied(w)
		return
	}
	var req struct {
		Tier string `json:"tier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON")
		return
	}
	if req.Tier == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "tier required")
		return
	}

	if err := s.vault.SetSensitivity(id, req.Tier); err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /vault/tokens/service
func (s *Server) handleCreateServiceToken(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	var req struct {
		Consumer       string `json:"consumer"`
		Scope          string `json:"scope"`
		TTL            string `json:"ttl"`
		MaxSensitivity string `json:"max_sensitivity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON")
		return
	}
	if req.Consumer == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "consumer required")
		return
	}
	if req.MaxSensitivity == "" {
		req.MaxSensitivity = vault.DefaultServiceTokenMaxSensitivity
	}

	ttl := vault.DefaultServiceTokenTTL
	if req.TTL != "" {
		parsed, err := time.ParseDuration(req.TTL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid ttl duration")
			return
		}
		ttl = parsed
	}

	token, err := s.vault.CreateServiceToken(req.Consumer, req.Scope, ttl, req.MaxSensitivity)
	if err != nil {
		handleVaultError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token":           token,
		"scope":           req.Scope,
		"max_sensitivity": req.MaxSensitivity,
		"expires_at":      time.Now().Add(ttl).UTC().Format(time.RFC3339),
	})
}

// GET /vault/tokens/service
func (s *Server) handleListServiceTokens(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	tokens, err := s.vault.ListServiceTokens()
	if err != nil {
		handleVaultError(w, err)
		return
	}

	type tokenInfo struct {
		TokenPrefix    string `json:"token_prefix"`
		Consumer       string `json:"consumer"`
		Scope          string `json:"scope"`
		MaxSensitivity string `json:"max_sensitivity"`
		ExpiresAt      string `json:"expires_at"`
		CreatedAt      string `json:"created_at"`
	}

	result := make([]tokenInfo, len(tokens))
	for i, t := range tokens {
		// TokenStr is a hash; its prefix identifies the token for revocation
		hashPrefix := t.TokenStr
		if len(hashPrefix) > 12 {
			hashPrefix = hashPrefix[:12] + "..."
		}
		result[i] = tokenInfo{
			TokenPrefix:    hashPrefix,
			Consumer:       t.Consumer,
			Scope:          t.Scope,
			MaxSensitivity: t.MaxSensitivity,
			ExpiresAt:      t.ExpiresAt.UTC().Format(time.RFC3339),
			CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// DELETE /vault/tokens/service/{token}
func (s *Server) handleRevokeServiceToken(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	ref := r.PathValue("token")
	if ref == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token or token prefix required")
		return
	}

	n, err := s.vault.RevokeServiceToken(ref)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "no matching token found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "count": n})
}

func handleVaultError(w http.ResponseWriter, err error) {
	var verr *vault.ValidationError
	if errors.As(err, &verr) {
		writeError(w, http.StatusBadRequest, "invalid_value", verr.Error())
		return
	}
	switch err {
	case vault.ErrForbiddenField:
		writeError(w, http.StatusBadRequest, "forbidden_field", err.Error())
	case vault.ErrAmbiguousToken:
		writeError(w, http.StatusConflict, "ambiguous_token", err.Error())
	case vault.ErrLocked:
		writeError(w, http.StatusForbidden, "vault_locked", "vault is locked")
	case vault.ErrAlreadyUnlocked:
		writeError(w, http.StatusConflict, "conflict", "vault is already unlocked")
	case vault.ErrNotInitialized:
		writeError(w, http.StatusPreconditionFailed, "not_initialized", "vault is not initialized")
	case vault.ErrInvalidTier:
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
