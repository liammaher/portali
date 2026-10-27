package api

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/store"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

type contextKey string

const (
	scopeKey       contextKey = "scope"
	sessionAuthKey contextKey = "session_auth"
	maxTierKey     contextKey = "max_tier"
	accessKey      contextKey = "access"
	consumerKey    contextKey = "consumer"
	tokenIDKey     contextKey = "token_id"
	signGrantKey   contextKey = "sign_grant"
)

// scopeFromRequest returns the token scope. Session tokens get "*" (full access).
func scopeFromRequest(r *http.Request) string {
	if s, ok := r.Context().Value(scopeKey).(string); ok {
		return s
	}
	return "*"
}

// maxTierFromRequest returns the highest sensitivity tier the caller may read.
// Session tokens get "personal"; requests without auth context get "public".
func maxTierFromRequest(r *http.Request) string {
	if t, ok := r.Context().Value(maxTierKey).(string); ok {
		return t
	}
	return "public"
}

// reachesPersonal reports whether the caller may see personal items: the
// user's session or a scoped grant they approved, never a service token.
func reachesPersonal(r *http.Request) bool {
	return maxTierFromRequest(r) == vault.TierPersonal
}

// accessFromRequest returns the caller's access level. Session tokens get
// write; requests without auth context get read.
func accessFromRequest(r *http.Request) string {
	if a, ok := r.Context().Value(accessKey).(string); ok {
		return a
	}
	return vault.AccessRead
}

// consumerFromRequest names the caller in the audit log and in who asked for
// a signature: "session", "grant", or a service token's consumer name.
func consumerFromRequest(r *http.Request) string {
	c, _ := r.Context().Value(consumerKey).(string)
	return c
}

// tokenIDFromRequest tells one service token from another of the same consumer
// name ("" for the user's own credentials).
func tokenIDFromRequest(r *http.Request) string {
	id, _ := r.Context().Value(tokenIDKey).(string)
	return id
}

// signGrantFromRequest returns the signing request a sign grant is for, or
// "" when the caller holds another kind of credential.
func signGrantFromRequest(r *http.Request) string {
	id, _ := r.Context().Value(signGrantKey).(string)
	return id
}

// isSessionAuth returns true if the request was authenticated with a session token.
func isSessionAuth(r *http.Request) bool {
	v, _ := r.Context().Value(sessionAuthKey).(bool)
	return v
}

func sessionRequired(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "session_required", "this operation requires a session token, not a service token")
}

// localHostMiddleware rejects requests whose Host header isn't a loopback
// name. The server only listens on 127.0.0.1, but a DNS-rebinding web page
// can still reach it under its own domain and would otherwise be treated as
// same-origin (sending JSON to /vault/session without a CORS preflight).
func localHostMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		switch strings.Trim(strings.ToLower(host), "[]") {
		case "127.0.0.1", "localhost", "::1":
			next.ServeHTTP(w, r)
		default:
			writeError(w, http.StatusForbidden, "invalid_host", "requests must use 127.0.0.1 or localhost")
		}
	})
}

// securityHeadersMiddleware sets standard security headers on all responses.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

const maxBodySize = 1 << 20 // 1 MB

// bodySizeMiddleware limits request body size to prevent memory exhaustion.
// Document uploads get a larger limit than JSON requests.
func bodySizeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			limit := int64(maxBodySize)
			if r.Method == http.MethodPost && r.URL.Path == "/vault/documents" {
				limit = vault.MaxDocumentSize
			}
			if r.Method == http.MethodPost && r.URL.Path == "/vault/signing" {
				limit = vault.MaxSigningPDF*4/3 + maxBodySize // base64 and the placements
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware extracts the Bearer token and validates it.
// Accepts both session tokens and service tokens.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "missing authorization")
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")

		// Try session token first — full access. Only user activity resets
		// the auto-lock timer.
		if s.vault.ValidateToken(token) {
			s.vault.TouchSession()
			serveAuthed(next, w, r, "session", "*", true, vault.TierPersonal, vault.AccessWrite)
			return
		}

		// Try a scoped grant — the user approved exactly this scope in
		// person, so it reaches personal fields, but it is not session auth.
		// Only the approval itself resets the auto-lock timer, so an agent
		// can't keep the vault open by polling with it.
		if scope, access, ok := s.vault.ScopedGrant(token); ok {
			serveAuthed(next, w, r, "grant", scope, false, vault.TierPersonal, access)
			return
		}

		// A sign grant is the user's approval of one signing request. It
		// reads and approves that request and nothing else, so any other
		// path is refused here, before a handler could mistake it for a
		// credential of another kind.
		if id, ok := s.vault.SigningGrant(token); ok {
			if !signGrantMayReach(r, id) {
				writeError(w, http.StatusForbidden, "scope_exceeded", "a sign grant only reads and approves its own signing request")
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), signGrantKey, id))
			serveAuthed(next, w, r, "grant", "", false, "public", vault.AccessRead)
			return
		}

		// Try service token — scoped access, capped sensitivity, and no
		// auto-lock reset so an agent cannot keep the vault open.
		if svcToken, ok := s.vault.ValidateServiceToken(token); ok {
			s.vault.LogAccess(store.AuditEntry{
				Consumer: svcToken.Consumer,
				Scope:    svcToken.Scope,
				Action:   "api_access",
			})
			// The stored hash names this token; its first 16 characters are enough.
			r = r.WithContext(context.WithValue(r.Context(), tokenIDKey, svcToken.TokenStr[:16]))
			serveAuthed(next, w, r, svcToken.Consumer, svcToken.Scope, false, svcToken.MaxSensitivity, svcToken.Access)
			return
		}

		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid or expired token")
	})
}

// serveAuthed runs next with what the caller may reach: its scope, whether
// it is the user's own session, its tier cap and its access level.
func serveAuthed(next http.Handler, w http.ResponseWriter, r *http.Request, consumer, scope string, session bool, maxTier, access string) {
	ctx := context.WithValue(r.Context(), consumerKey, consumer)
	ctx = context.WithValue(ctx, scopeKey, scope)
	ctx = context.WithValue(ctx, sessionAuthKey, session)
	ctx = context.WithValue(ctx, maxTierKey, maxTier)
	ctx = context.WithValue(ctx, accessKey, access)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// signGrantMayReach reports whether r is one of the two requests a sign grant
// for request id may make.
func signGrantMayReach(r *http.Request, id string) bool {
	base := "/vault/signing/" + id
	return (r.Method == http.MethodGet && r.URL.Path == base+"/candidate") ||
		(r.Method == http.MethodPost && r.URL.Path == base+"/approve")
}
