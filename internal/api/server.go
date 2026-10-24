package api

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// rateLimiter tracks attempts within a time window.
type rateLimiter struct {
	mu       sync.Mutex
	attempts []time.Time
	max      int
	window   time.Duration
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window}
}

// allow returns true if the request is within the rate limit, counting it.
func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.fullLocked() {
		return false
	}
	rl.attempts = append(rl.attempts, time.Now())
	return true
}

// refund uncounts one attempt previously admitted by allow. Pairing the two
// limits only failed attempts while still counting in-flight ones, so
// concurrent requests can't all slip past the limit before any fails.
func (rl *rateLimiter) refund() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if n := len(rl.attempts); n > 0 {
		rl.attempts = rl.attempts[:n-1]
	}
}

// fullLocked drops expired attempts and reports whether the limit is reached.
// Caller must hold rl.mu.
func (rl *rateLimiter) fullLocked() bool {
	cutoff := time.Now().Add(-rl.window)
	valid := rl.attempts[:0]
	for _, t := range rl.attempts {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	rl.attempts = valid
	return len(rl.attempts) >= rl.max
}

// Server is the HTTP API server for the vault.
type Server struct {
	vault       *vault.Vault
	mux         *http.ServeMux
	handler     http.Handler // full chain: bodySizeMiddleware → mux
	server      *http.Server
	unlockLimit *rateLimiter
	grantLimit  *rateLimiter
	uploadSlots chan struct{} // bounds concurrent document uploads
}

// maxConcurrentUploads caps memory used by in-flight document uploads.
const maxConcurrentUploads = 2

// New creates a new API server.
func New(v *vault.Vault, addr string) *Server {
	s := &Server{
		vault:       v,
		unlockLimit: newRateLimiter(5, time.Minute),
		grantLimit:  newRateLimiter(10, time.Minute),
		uploadSlots: make(chan struct{}, maxConcurrentUploads),
	}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	s.handler = localHostMiddleware(securityHeadersMiddleware(bodySizeMiddleware(s.mux)))
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.handler,
	}
	return s
}

func (s *Server) registerRoutes() {
	// Public endpoints (no auth required)
	s.mux.HandleFunc("GET /ui", s.handleUI)
	s.mux.HandleFunc("GET /ui/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui", http.StatusMovedPermanently)
	})
	s.mux.HandleFunc("POST /vault/unlock", s.handleUnlock)
	s.mux.HandleFunc("POST /vault/session", s.handleSessionGrant)
	s.mux.HandleFunc("GET /vault/status", s.handleStatus)
	s.mux.HandleFunc("GET /vault/schema", s.handleSchema)

	// Protected endpoints
	protected := http.NewServeMux()
	protected.HandleFunc("POST /vault/lock", s.handleLock)
	protected.HandleFunc("GET /vault/fields", s.handleListFields)
	protected.HandleFunc("GET /vault/fields/category/{category}", s.handleGetByCategory)
	protected.HandleFunc("GET /vault/fields/{id...}", s.handleGetField)
	protected.HandleFunc("PUT /vault/fields/{id...}", s.handleSetField)
	protected.HandleFunc("DELETE /vault/fields/{id...}", s.handleDeleteField)
	protected.HandleFunc("GET /vault/context", s.handleGetContext)
	protected.HandleFunc("GET /vault/expiring", s.handleExpiring)
	protected.HandleFunc("POST /vault/documents", s.handleAddDocument)
	protected.HandleFunc("GET /vault/documents", s.handleListDocuments)
	protected.HandleFunc("GET /vault/documents/{id}", s.handleGetDocument)
	protected.HandleFunc("DELETE /vault/documents/{id}", s.handleDeleteDocument)
	protected.HandleFunc("GET /vault/audit", s.handleAuditLog)
	protected.HandleFunc("PUT /vault/sensitivity/{id...}", s.handleSetSensitivity)
	protected.HandleFunc("POST /vault/tokens/service", s.handleCreateServiceToken)
	protected.HandleFunc("GET /vault/tokens/service", s.handleListServiceTokens)
	protected.HandleFunc("DELETE /vault/tokens/service/{token}", s.handleRevokeServiceToken)

	s.mux.Handle("/", s.authMiddleware(protected))
}

// Start begins listening. Returns immediately; use the returned listener to get the actual port.
func (s *Server) Start() (net.Listener, error) {
	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return nil, err
	}
	go s.server.Serve(ln)
	return ln, nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
