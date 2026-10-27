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

// allow reports whether the request is within the rate limit, counting it,
// and returns the attempt for refund.
func (rl *rateLimiter) allow() (time.Time, bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.fullLocked() {
		return time.Time{}, false
	}
	now := time.Now()
	rl.attempts = append(rl.attempts, now)
	return now, true
}

// refund uncounts an attempt admitted by allow. Pairing the two limits only
// failed attempts while still counting in-flight ones, so concurrent
// requests can't all slip past the limit before any fails. An attempt that
// already expired is gone; refunding it must not erase a newer one.
func (rl *rateLimiter) refund(attempt time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for i, t := range rl.attempts {
		if t.Equal(attempt) {
			rl.attempts = append(rl.attempts[:i], rl.attempts[i+1:]...)
			return
		}
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
	// approver asks the user in person (Windows Hello); approveMu keeps it
	// to one prompt at a time.
	approver  Approver
	approveMu sync.Mutex
	// stopping ends every request's context when Stop begins, so a prompt
	// still open doesn't keep a stopping server alive.
	stopping   context.Context
	stopAsking context.CancelFunc
	// stopped is closed by POST /vault/stop (see Stopped).
	stopped  chan struct{}
	stopOnce sync.Once
	// tcpAddr is the address Start listens on. Status reports it, so a client
	// that verified the server over its Unix socket can tell whether its TCP
	// address reaches the same server or something else.
	tcpAddr string
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
		stopped:     make(chan struct{}),
	}
	s.stopping, s.stopAsking = context.WithCancel(context.Background())
	s.mux = http.NewServeMux()
	s.registerRoutes()
	s.handler = localHostMiddleware(securityHeadersMiddleware(bodySizeMiddleware(s.mux)))
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.handler,
		// Marks requests that arrived on the private channel, a Unix socket
		// or a Windows named pipe, which the sign gate's password route and
		// summary are limited to (see viaSocket).
		BaseContext: func(net.Listener) context.Context { return s.stopping },
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			network := c.LocalAddr().Network()
			return context.WithValue(ctx, socketKey{}, network == "unix" || network == "pipe")
		},
		// A request that trickles in can't hold an upload slot or a rate
		// limit attempt indefinitely. Uploads are at most 20 MB over loopback.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	return s
}

func (s *Server) registerRoutes() {
	// Public endpoints (no auth required)
	s.mux.HandleFunc("GET /ui", s.handleUI)
	s.mux.HandleFunc("GET /ui/fields", s.handleUIFields)
	s.mux.HandleFunc("GET /ui/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui", http.StatusMovedPermanently)
	})
	s.mux.HandleFunc("POST /vault/unlock", s.handleUnlock)
	s.mux.HandleFunc("POST /vault/stop", s.handleStop)
	s.mux.HandleFunc("POST /vault/session", s.handleSessionGrant)
	s.mux.HandleFunc("GET /vault/status", s.handleStatus)
	s.mux.HandleFunc("GET /vault/schema", s.handleSchema)
	s.mux.HandleFunc("GET /vault/signing/{id}/summary", s.handleSigningSummary)

	// Protected endpoints
	protected := http.NewServeMux()
	protected.HandleFunc("POST /vault/lock", s.handleLock)
	protected.HandleFunc("POST /vault/hello", s.handleHello)
	protected.HandleFunc("GET /vault/fields", s.handleListFields)
	protected.HandleFunc("GET /vault/fields/category/{category}", s.handleGetByCategory)
	protected.HandleFunc("GET /vault/fields/{id...}", s.handleGetField)
	protected.HandleFunc("PUT /vault/fields/{id...}", s.handleSetField)
	protected.HandleFunc("DELETE /vault/fields/{id...}", s.handleDeleteField)
	protected.HandleFunc("GET /vault/context", s.handleGetContext)
	protected.HandleFunc("GET /vault/located/{target...}", s.handleLocated)
	protected.HandleFunc("GET /vault/expiring", s.handleExpiring)
	protected.HandleFunc("GET /vault/members", s.handleListMembers)
	protected.HandleFunc("DELETE /vault/members/{slug}", s.handleRemovePerson)
	protected.HandleFunc("POST /vault/members/migrate", s.handleMigrateMembers)
	protected.HandleFunc("GET /vault/members/{slug}/export", s.handleExportPerson)
	protected.HandleFunc("GET /vault/retention", s.handleGetRetention)
	protected.HandleFunc("PUT /vault/retention", s.handleSetRetention)
	protected.HandleFunc("POST /vault/documents", s.handleAddDocument)
	protected.HandleFunc("GET /vault/documents", s.handleListDocuments)
	protected.HandleFunc("GET /vault/documents/{id}", s.handleGetDocument)
	protected.HandleFunc("DELETE /vault/documents/{id}", s.handleDeleteDocument)
	protected.HandleFunc("GET /vault/files", s.handleSearchFiles)
	protected.HandleFunc("POST /vault/files/sync", s.handleSyncFiles)
	protected.HandleFunc("POST /vault/files/sync/finish", s.handleFinishFileSync)
	protected.HandleFunc("POST /vault/files/prune", s.handlePruneFiles)
	protected.HandleFunc("GET /vault/files/sources", s.handleListFileSources)
	protected.HandleFunc("POST /vault/files/sources", s.handleAddFileSource)
	protected.HandleFunc("DELETE /vault/files/sources/{id}", s.handleForgetFileSource)
	protected.HandleFunc("POST /vault/files/sources/{id}/state", s.handleFileSourceState)
	protected.HandleFunc("GET /vault/files/rules", s.handleListFileRules)
	protected.HandleFunc("POST /vault/files/rules", s.handleAddFileRule)
	protected.HandleFunc("DELETE /vault/files/rules/{id}", s.handleRemoveFileRule)
	protected.HandleFunc("GET /vault/files/suggestions", s.handleListFileSuggestions)
	protected.HandleFunc("POST /vault/files/suggestions/{id}/{action}", s.handleResolveFileSuggestion)
	protected.HandleFunc("GET /vault/files/proposals", s.handleListFileProposals)
	protected.HandleFunc("POST /vault/files/proposals", s.handleProposeFile)
	protected.HandleFunc("POST /vault/files/proposals/{id}/{action}", s.handleResolveFileProposal)
	protected.HandleFunc("DELETE /vault/files/links/{id}", s.handleUnlinkFile)
	protected.HandleFunc("GET /vault/files/{id}", s.handleGetFile)
	protected.HandleFunc("POST /vault/files/{id}/links", s.handleLinkFile)
	protected.HandleFunc("POST /vault/files/{id}/suggest", s.handleSuggestFileLink)
	protected.HandleFunc("POST /vault/signing", s.handleSigningRequest)
	protected.HandleFunc("GET /vault/signing", s.handleSigningList)
	protected.HandleFunc("GET /vault/signing/{id}", s.handleSigningStatus)
	protected.HandleFunc("GET /vault/signing/{id}/candidate", s.handleSigningCandidate)
	protected.HandleFunc("POST /vault/signing/{id}/approve", s.handleSigningApprove)
	protected.HandleFunc("DELETE /vault/signing/{id}", s.handleSigningReject)
	protected.HandleFunc("GET /vault/audit", s.handleAuditLog)
	protected.HandleFunc("POST /vault/backup", s.handleBackup)
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
	s.tcpAddr = ln.Addr().String()
	go s.server.Serve(ln)
	return ln, nil
}

// Serve also serves requests from ln, e.g. a Unix socket next to the TCP
// listener. Stop closes it too.
func (s *Server) Serve(ln net.Listener) {
	go s.server.Serve(ln)
}

// Approver asks the person at this computer to approve a request in person,
// with a prompt showing message over window (0 for none): Windows Hello.
type Approver interface {
	// Available reports why it can't ask now, or nil.
	Available() error
	// Approve stops asking, and refuses, once ctx is done.
	Approve(ctx context.Context, window uint64, message string) (bool, error)
}

// SetApprover lets a can ask the user to approve grants (approve "hello").
func (s *Server) SetApprover(a Approver) { s.approver = a }

// Stopped is closed when POST /vault/stop asks the server to lock and exit.
func (s *Server) Stopped() <-chan struct{} { return s.stopped }

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	s.stopAsking()
	return s.server.Shutdown(ctx)
}
