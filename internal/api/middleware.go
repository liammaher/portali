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

// consumerFromRequest names the caller for records others read later (who
// proposed a file link): the service token's consumer, else "you".
func consumerFromRequest(r *http.Request) string {
	if c, ok := r.Context().Value(consumerKey).(string); ok {
		return c
	}
	return "you"
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
			serveAuthed(next, w, r, "*", true, vault.TierPersonal, vault.AccessWrite)
			return
		}

		// Try a scoped grant — the user approved exactly this scope in
		// person, so it reaches personal fields, but it is not session auth.
		// Only the approval itself resets the auto-lock timer, so an agent
		// can't keep the vault open by polling with it.
		if scope, access, ok := s.vault.ScopedGrant(token); ok {
			serveAuthed(next, w, r, scope, false, vault.TierPersonal, access)
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
			r = r.WithContext(context.WithValue(r.Context(), consumerKey, svcToken.Consumer))
			serveAuthed(next, w, r, svcToken.Scope, false, svcToken.MaxSensitivity, svcToken.Access)
			return
		}

		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid or expired token")
	})
}

// serveAuthed runs next with what the caller may reach: its scope, whether
// it is the user's own session, its tier cap and its access level.
func serveAuthed(next http.Handler, w http.ResponseWriter, r *http.Request, scope string, session bool, maxTier, access string) {
	ctx := context.WithValue(r.Context(), scopeKey, scope)
	ctx = context.WithValue(ctx, sessionAuthKey, session)
	ctx = context.WithValue(ctx, maxTierKey, maxTier)
	ctx = context.WithValue(ctx, accessKey, access)
	next.ServeHTTP(w, r.WithContext(ctx))
}
