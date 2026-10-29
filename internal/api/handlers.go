package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

func scopeDenied(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "scope_exceeded", "token scope does not allow access to this field")
}

// requireScope checks the caller's scope allows every one of ids.
// readJSONBody decodes the request body into v, or answers 413 for a body over
// the server's limit (bodySizeMiddleware) and 400 for anything else.
func readJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(r.Body).Decode(v)
	var tooLarge *http.MaxBytesError
	switch {
	case err == nil:
		return true
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the request body is too large")
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
	}
	return false
}

func requireScope(w http.ResponseWriter, r *http.Request, ids ...string) bool {
	scope := scopeFromRequest(r)
	for _, id := range ids {
		if !vault.ScopeAllows(scope, id) {
			scopeDenied(w)
			return false
		}
	}
	return true
}

// requireAccess checks the caller may make a change. Adding something new
// needs append or write; overwriting or deleting needs write.
func requireAccess(w http.ResponseWriter, r *http.Request, overwrite bool) bool {
	switch access := accessFromRequest(r); {
	case access == vault.AccessWrite:
		return true
	case access == vault.AccessAppend && !overwrite:
		return true
	case access == vault.AccessAppend:
		writeError(w, http.StatusForbidden, "append_only", "this token can add new fields and documents but not overwrite or delete them")
	default:
		writeError(w, http.StatusForbidden, "read_only", "this token is read-only")
	}
	return false
}

func sensitivityDenied(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "sensitivity_exceeded", "token is not allowed to access fields at this sensitivity tier")
}

func personalOnlyDenied(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "personal_only", "personal fields and documents can only be changed by you, never by an agent")
}

// requireFieldTier checks an existing field's tier against the caller's cap
// and writes the error response if access is denied. Missing fields pass so
// callers can return their usual 404 or create them. Personal fields are
// invisible to service tokens: reads and deletes get 404, writes are refused.
//
// It returns the field's metadata (nil if missing, and always nil for the
// user's own session, which skips the check) so callers needn't look it up again.
// A scoped grant the user approved in person may reach any tier.
func (s *Server) requireFieldTier(w http.ResponseWriter, r *http.Request, id string, write bool) (*vault.FieldInfo, bool) {
	if isSessionAuth(r) {
		return nil, true
	}
	maxTier := maxTierFromRequest(r)
	meta, err := s.vault.FieldMeta(id)
	if err != nil {
		handleVaultError(w, err)
		return nil, false
	}
	switch {
	case meta == nil, maxTier == vault.TierPersonal:
		return meta, true
	case meta.Sensitivity == vault.TierPersonal && write:
		personalOnlyDenied(w)
		return nil, false
	case meta.Sensitivity == vault.TierPersonal:
		writeError(w, http.StatusNotFound, "not_found", "field not found")
		return nil, false
	case !vault.TierAllows(maxTier, meta.Sensitivity):
		sensitivityDenied(w)
		return nil, false
	}
	return meta, true
}

// callerFilter limits bulk reads to fields within the caller's scope and
// sensitivity cap, so nothing else is decrypted.
func callerFilter(r *http.Request) vault.FieldFilter {
	scope, maxTier := vault.ParseScope(scopeFromRequest(r)), maxTierFromRequest(r)
	return func(id, sensitivity string) bool {
		return scope.Allows(id) && vault.TierAllows(maxTier, sensitivity)
	}
}

// peopleFilter is what the caller may know about who is in the vault: nil
// (everyone) for the user's session and grants they approved in person, the
// caller's own filter for a service token, so its answers never depend on
// people it can't see.
func peopleFilter(r *http.Request) vault.FieldFilter {
	if isUser(r) {
		return nil
	}
	return callerFilter(r)
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
	attempt, ok := s.unlockLimit.allow()
	if !ok {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many unlock attempts, try again later")
		return
	}
	// As with grants, only a wrong password or secret key keeps the attempt, so
	// junk requests can't use up the user's own unlocks.
	wrongPassword := false
	defer func() {
		if !wrongPassword {
			s.unlockLimit.refund(attempt)
		}
	}()

	var req struct {
		Password  string `json:"password"`
		SecretKey string `json:"secret_key"`
	}
	if !readJSONBody(w, r, &req) {
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
			wrongPassword = true
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
	attempt, ok := s.grantLimit.allow()
	if !ok {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	}
	// The attempt is counted up front; only a wrong password, or a request
	// the user refused in person, keeps it.
	wrongPassword := false
	defer func() {
		if !wrongPassword {
			s.grantLimit.refund(attempt)
		}
	}()

	var req sessionGrantRequest
	if !readJSONBody(w, r, &req) {
		return
	}
	if req.Approve != "" {
		wrongPassword = s.approvedGrant(w, r, req)
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

	var token string
	var expires time.Time
	var err error
	if req.Sign != "" {
		switch {
		case req.Scope != "" || req.Access != "" || req.TTL != "":
			writeError(w, http.StatusBadRequest, "invalid_request", "sign can't be combined with scope, access or ttl")
			return
		case !viaSocket(r):
			writeError(w, http.StatusForbidden, "socket_required", "a signing approval is requested only over the vault's Unix socket")
			return
		}
		token, expires, err = s.vault.CreateSignGrant(req.Password, req.SecretKey, req.Sign)
	} else if req.Scope != "" {
		token, expires, err = s.vault.CreateScopedGrant(req.Password, req.SecretKey, ttl, req.Scope, req.Access)
	} else if req.Access != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "access requires a scope")
		return
	} else {
		token, expires, err = s.vault.CreateSessionGrant(req.Password, req.SecretKey, ttl)
	}
	if err != nil {
		if err == vault.ErrWrongPassword {
			wrongPassword = true
			writeError(w, http.StatusUnauthorized, "unauthenticated", "wrong password or secret key")
			return
		}
		handleVaultError(w, err)
		return
	}
	writeGrant(w, token, expires, req)
}

type sessionGrantRequest struct {
	Password  string `json:"password"`
	SecretKey string `json:"secret_key"`
	TTL       string `json:"ttl"`
	// Scope and Access, when set, limit the grant like a service token
	// (see CreateScopedGrant).
	Scope  string `json:"scope"`
	Access string `json:"access"`
	// Sign asks for a grant that can only review and approve this
	// signing request (see CreateSignGrant); socket only.
	Sign string `json:"sign"`
	// Approve "hello" asks the user in person instead of taking a password
	// (see approvedGrant); Window is the window to show the prompt over.
	Approve string `json:"approve"`
	Window  uint64 `json:"window"`
}

// writeGrant answers a grant request with its token, echoing sign, scope
// and access so a client can tell what it got (older servers ignore them
// and grant full access).
func writeGrant(w http.ResponseWriter, token string, expires time.Time, req sessionGrantRequest) {
	resp := map[string]string{
		"token":      token,
		"expires_at": expires.UTC().Format(time.RFC3339),
	}
	if req.Sign != "" {
		resp["sign"] = req.Sign
	}
	if req.Scope != "" {
		resp["scope"] = req.Scope
		resp["access"] = req.Access
		if resp["access"] == "" {
			resp["access"] = vault.AccessRead
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// approvedGrant answers POST /vault/session with approve "hello": the server
// asks the user itself, with Windows Hello, in a prompt worded from this very
// request (vault.GrantPrompt, or vault.SignPrompt from the vault's own
// summary), and issues exactly it. Only over the private channel, only when
// the user turned it on, never with a password, one prompt at a time. It
// reports whether the user refused, which counts toward the rate limit.
func (s *Server) approvedGrant(w http.ResponseWriter, r *http.Request, req sessionGrantRequest) (refused bool) {
	switch {
	case req.Approve != "hello":
		writeError(w, http.StatusBadRequest, "invalid_request", `approve must be "hello"`)
		return false
	case req.Password != "" || req.SecretKey != "":
		writeError(w, http.StatusBadRequest, "invalid_request", "approve takes no password")
		return false
	case !viaSocket(r):
		writeError(w, http.StatusForbidden, "socket_required", "an approval is requested only over the vault's socket")
		return false
	}
	if status, err := s.vault.Status(); err != nil || status.Locked {
		handleVaultError(w, vault.ErrLocked)
		return false
	}
	if on, err := s.vault.HelloEnabled(); err != nil || !on {
		writeError(w, http.StatusPreconditionFailed, "approver_off", "Windows Hello isn't turned on for this vault")
		return false
	}
	if s.approver == nil {
		writeError(w, http.StatusServiceUnavailable, "approver_unavailable", "Windows Hello isn't available")
		return false
	}
	if err := s.approver.Available(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "approver_unavailable", err.Error())
		return false
	}

	label := s.vault.PromptLabel()
	var reason, scope string
	ttl := 2 * time.Minute
	if req.Sign != "" {
		if req.Scope != "" || req.Access != "" || req.TTL != "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "sign can't be combined with scope, access or ttl")
			return false
		}
		info, err := s.vault.SigningStatus(req.Sign, "")
		if err == nil && info.Status != vault.SigningPending {
			err = vault.ErrSigningNotPending
		}
		if err != nil {
			handleVaultError(w, err)
			return false
		}
		reason = vault.SignPrompt(label, info.Name, info.Pages, info.SHA256[:8], info.Requester, vault.SignGrantTTL)
		scope = vault.SigningScopeID
	} else {
		if req.TTL != "" {
			parsed, err := time.ParseDuration(req.TTL)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "invalid ttl duration")
				return false
			}
			ttl = parsed
		}
		if _, err := vault.CheckGrantRequest(ttl, req.Scope, req.Access); err != nil {
			handleVaultError(w, err)
			return false
		}
		var err error
		if reason, err = vault.GrantPrompt(label, req.Scope, req.Access, ttl); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return false
		}
		scope = req.Scope
		if scope == "" {
			scope = "*"
		}
	}

	if !s.approveMu.TryLock() {
		writeError(w, http.StatusConflict, "busy", "another approval is waiting for you")
		return false
	}
	defer s.approveMu.Unlock()
	approved, err := s.approver.Approve(r.Context(), req.Window, "pvault wants to "+reason+".")
	// Whoever asked has gone, or the server is stopping: no grant.
	if r.Context().Err() != nil {
		writeError(w, http.StatusServiceUnavailable, "approver_unavailable", "the request was cancelled")
		return false
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "approver_unavailable", err.Error())
		return false
	}
	if !approved {
		s.vault.AuditApprovalRefused("hello", scope, reason)
		writeError(w, http.StatusForbidden, "not_approved", "the request was not approved")
		return true
	}
	// Turned off while the prompt was open: the user meant it to stop.
	if on, err := s.vault.HelloEnabled(); err != nil || !on {
		writeError(w, http.StatusPreconditionFailed, "approver_off", "Windows Hello was turned off for this vault")
		return false
	}
	var token string
	var expires time.Time
	if req.Sign != "" {
		token, expires, err = s.vault.CreateApprovedSignGrant(req.Sign, "hello")
	} else {
		token, expires, err = s.vault.CreateApprovedGrant(ttl, req.Scope, req.Access, "hello")
	}
	if err != nil {
		handleVaultError(w, err)
		return false
	}
	writeGrant(w, token, expires, req)
	return false
}

// POST /vault/hello turns Windows Hello approvals on or off. Only the session
// (the typed password, or full access the user approved) may, over the private
// channel as pvault hello sends it (not the onboarding page), and it is turned
// on only where Windows Hello can answer.
func (s *Server) handleHello(w http.ResponseWriter, r *http.Request) {
	if !viaSocket(r) {
		writeError(w, http.StatusForbidden, "socket_required", "Windows Hello is turned on or off only with pvault hello")
		return
	}
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	if !requireJSON(w, r) {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "enabled required")
		return
	}
	if *req.Enabled {
		err := errors.New("Windows Hello isn't available")
		if s.approver != nil {
			err = s.approver.Available()
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "approver_unavailable", err.Error())
			return
		}
	}
	if err := s.vault.SetHelloEnabled(*req.Enabled); err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": *req.Enabled})
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

// POST /vault/stop asks the server to lock and exit, as SIGTERM does: on
// Windows, which has none, pvault lock asks this way. Only the private channel
// reaches it, so only this user, who can signal the server anyway, and like
// SIGTERM it needs no password.
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if !viaSocket(r) {
		http.NotFound(w, r)
		return
	}
	s.stopOnce.Do(func() { close(s.stopped) })
	w.WriteHeader(http.StatusAccepted)
}

// GET /vault/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.vault.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	// Over the private channel only: whether Windows Hello approves grants,
	// so the CLI knows to ask for it.
	approver := ""
	if on, _ := s.vault.HelloEnabled(); on && viaSocket(r) {
		approver = "hello"
	}
	writeJSON(w, http.StatusOK, struct {
		*vault.VaultStatus
		Addr     string `json:"addr,omitempty"`
		Approver string `json:"approver,omitempty"`
	}{status, s.tcpAddr, approver})
}

// GET /vault/fields
func (s *Server) handleListFields(w http.ResponseWriter, r *http.Request) {
	fields, err := s.vault.List()
	if err != nil {
		handleVaultError(w, err)
		return
	}
	scope, maxTier := vault.ParseScope(scopeFromRequest(r)), maxTierFromRequest(r)
	allowed := make([]vault.FieldInfo, 0, len(fields))
	for _, f := range fields {
		// Agents see metadata of fields above their cap, but personal fields
		// don't exist for them at all.
		if f.Sensitivity == vault.TierPersonal && maxTier != vault.TierPersonal {
			continue
		}
		if scope.Allows(f.ID) {
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
	if !requireScope(w, r, id) {
		return
	}
	if _, ok := s.requireFieldTier(w, r, id, false); !ok {
		return
	}
	// The filter re-checks the tier on the row actually decrypted, in case the
	// field was created or raised to personal since requireFieldTier looked.
	field, err := s.vault.GetIf(id, callerFilter(r))
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
	if !requireScope(w, r, id) {
		return
	}
	// A read-only token is refused before the field is looked up, so it
	// can't tell a hidden personal field from a missing one.
	if !isSessionAuth(r) && accessFromRequest(r) == vault.AccessRead {
		requireAccess(w, r, false)
		return
	}
	existing, ok := s.requireFieldTier(w, r, id, true)
	if !ok {
		return
	}
	if !isSessionAuth(r) {
		// Agents can never create personal fields (credentials, SPID
		// details) unless the user approved this one in person. An existing
		// field keeps its own tier, so one the user lowered (e.g.
		// spid.data_scadenza) can still be maintained.
		if existing == nil && vault.DefaultSensitivity(id) == vault.TierPersonal && !reachesPersonal(r) {
			personalOnlyDenied(w)
			return
		}
		if !requireAccess(w, r, existing != nil) {
			return
		}
	}
	var req struct {
		Value       string `json:"value"`
		Sensitivity string `json:"sensitivity"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "value required")
		return
	}

	// An empty tier keeps an existing field's tier, or uses the schema
	// default for a new field (see Vault.Set).
	if req.Sensitivity == vault.TierPersonal && !reachesPersonal(r) {
		personalOnlyDenied(w)
		return
	}
	// Only the user's session may lower a tier (as with /vault/sensitivity);
	// a token could otherwise expose a field to tokens with a lower cap.
	if req.Sensitivity != "" && !isSessionAuth(r) {
		baseline := vault.DefaultSensitivity(id)
		if existing != nil {
			baseline = existing.Sensitivity
		}
		if !vault.TierAllows(req.Sensitivity, baseline) {
			writeError(w, http.StatusForbidden, "session_required", "only your own session can lower a field's sensitivity tier")
			return
		}
	}

	// A token's checks above hold only for the field as it was looked up;
	// SetChecked refuses the write if it changed meanwhile.
	// A service token may name only people it can see (pratiche persona,
	// famiglia), so it can't find out who else is in the vault; a grant the
	// user approved in person may name anyone, as the session can.
	set := s.vault.Set
	if !isSessionAuth(r) {
		allow := peopleFilter(r)
		set = func(id, value, sensitivity string) error {
			return s.vault.SetChecked(id, value, sensitivity, existing, allow)
		}
	}
	if err := set(id, req.Value, req.Sensitivity); err != nil {
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
	// The schema is the same for every vault; vault_kind says which of its
	// categories this one's own data and people can hold (subject, kinds).
	writeJSON(w, http.StatusOK, struct {
		vault.Schema
		VaultKind string `json:"vault_kind"`
	}{vault.RecommendedSchema, s.vault.Kind()})
}

// DELETE /vault/fields/{id...}
func (s *Server) handleDeleteField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := vault.ValidateFieldID(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !requireScope(w, r, id) {
		return
	}
	del := s.vault.Delete
	if !isSessionAuth(r) {
		// Access first, so a read-only token learns nothing about the field.
		if !requireAccess(w, r, true) {
			return
		}
		existing, ok := s.requireFieldTier(w, r, id, false)
		if !ok {
			return
		}
		// Same answer as a hidden personal field, so tokens can't probe for them.
		if existing == nil {
			writeError(w, http.StatusNotFound, "not_found", "field not found")
			return
		}
		// Writing it again would give it the default tier, so deleting a field
		// stored above that would let an agent lower it; only the user's session may.
		if !vault.TierAllows(vault.DefaultSensitivity(id), existing.Sensitivity) {
			writeError(w, http.StatusForbidden, "session_required", "only your own session can delete a field stored above its default sensitivity tier")
			return
		}
		del = func(id string) error { return s.vault.DeleteChecked(id, existing) }
	}
	if err := del(id); err != nil {
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
	// ?omit=id,id keeps those values out of the answer (the onboarding page
	// never needs a saved secret back); the fields still show as saved.
	if omit := r.URL.Query().Get("omit"); omit != "" {
		ids := strings.Split(omit, ",")
		for i := range fields {
			if slices.Contains(ids, fields[i].ID) {
				fields[i].Value, fields[i].Redacted = "", true
			}
		}
	}
	writeJSON(w, http.StatusOK, fields)
}

// GET /vault/context
func (s *Server) handleGetContext(w http.ResponseWriter, r *http.Request) {
	get := s.vault.GetContext
	if r.URL.Query().Get("stored") == "1" {
		get = s.vault.GetStoredContext // an export: what is stored, not what the vault computes
	}
	ctx, err := get(callerFilter(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ctx)
}

// GET /vault/located/{target...}
// Says where an address of the vault is in Napoli: its quartiere, municipalità
// and ASL distretto, what settled them and, when they can't be known, why and
// which quartieri to ask about. target is residenza, domicilio, locale, immobile
// or sede_legale, with an optional person (leo:residenza). It answers under the
// scope of the three computed fields, never needing the street's.
func (s *Server) handleLocated(w http.ResponseWriter, r *http.Request) {
	category, ids, ok := vault.LocatedTarget(r.PathValue("target"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", vault.ErrNotLocatable.Error())
		return
	}
	if !requireScope(w, r, ids[:]...) {
		return
	}
	loc, err := s.vault.Locate(category, callerFilter(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, loc)
}

// GET /vault/expiring?days=90
// Lists document expiry dates that have passed or fall within the window.
func (s *Server) handleExpiring(w http.ResponseWriter, r *http.Request) {
	days := 90
	if d := r.URL.Query().Get("days"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 0 || n > 3650 {
			writeError(w, http.StatusBadRequest, "invalid_request", "days must be between 0 and 3650")
			return
		}
		days = n
	}
	items, err := s.vault.Expiring(time.Duration(days)*24*time.Hour, callerFilter(r), time.Now())
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
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

// POST /vault/backup
// Session only: writes an encrypted backup now. A "dir" in the body is also
// remembered as the folder for automatic backups.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !isSessionAuth(r) {
		sessionRequired(w)
		return
	}
	if !requireJSON(w, r) {
		return
	}
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON")
		return
	}
	if req.Dir != "" {
		if err := s.vault.SetBackupDir(req.Dir); err != nil {
			handleVaultError(w, err)
			return
		}
	}
	path, err := s.vault.Backup()
	switch {
	case errors.Is(err, vault.ErrNoBackupDir):
		writeError(w, http.StatusBadRequest, "no_backup_dir", err.Error())
	case errors.Is(err, vault.ErrLocked):
		handleVaultError(w, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]string{"path": path})
	}
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
	if !requireScope(w, r, id) {
		return
	}
	var req struct {
		Tier string `json:"tier"`
	}
	if !readJSONBody(w, r, &req) {
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
		Access         string `json:"access"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	if req.Consumer == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "consumer required")
		return
	}
	if req.MaxSensitivity == "" {
		req.MaxSensitivity = vault.DefaultServiceTokenMaxSensitivity
	}
	if req.Access == "" {
		req.Access = vault.AccessRead
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

	token, err := s.vault.CreateServiceToken(req.Consumer, req.Scope, ttl, req.MaxSensitivity, req.Access)
	if err != nil {
		handleVaultError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token":           token,
		"scope":           req.Scope,
		"max_sensitivity": req.MaxSensitivity,
		"access":          req.Access,
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
		Access         string `json:"access"`
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
			Access:         t.Access,
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
	var nr *vault.NotResolvedError
	if errors.As(err, &nr) {
		writeError(w, http.StatusNotFound, "not_resolved", nr.Error())
		return
	}
	switch err {
	case vault.ErrFieldNotFound:
		writeError(w, http.StatusNotFound, "not_found", "field not found")
	case vault.ErrFileNotFound:
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case vault.ErrPersonNotFound:
		writeError(w, http.StatusNotFound, "not_found", "person not found")
	case vault.ErrFieldChanged:
		writeError(w, http.StatusConflict, "changed", err.Error())
	case vault.ErrAmbiguousToken:
		writeError(w, http.StatusConflict, "ambiguous_token", err.Error())
	case vault.ErrSigningNotFound:
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case vault.ErrSigningNotPending, vault.ErrSigningStale:
		writeError(w, http.StatusConflict, "not_pending", err.Error())
	case vault.ErrSigningHash:
		writeError(w, http.StatusBadRequest, "hash_mismatch", err.Error())
	case vault.ErrSigningLimit, vault.ErrSigningRequesterLimit, vault.ErrSigningRate:
		writeError(w, http.StatusTooManyRequests, "too_many_pending", err.Error())
	case vault.ErrNoSignature:
		writeError(w, http.StatusConflict, "no_signature", err.Error())
	case vault.ErrLocked:
		writeError(w, http.StatusForbidden, "vault_locked", "vault is locked")
	case vault.ErrAlreadyUnlocked:
		writeError(w, http.StatusConflict, "conflict", "vault is already unlocked")
	case vault.ErrNotInitialized:
		writeError(w, http.StatusPreconditionFailed, "not_initialized", "vault is not initialized")
	case vault.ErrInvalidTier, vault.ErrInvalidAccess:
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
