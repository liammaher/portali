package vault

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

const defaultAutoLockDuration = 30 * time.Minute

// Session holds the in-memory vault key and session token.
type Session struct {
	mu       sync.Mutex
	token    string
	vaultKey []byte
	timer    *time.Timer
	timerGen uint64 // bumped per timer, so an older timer's firing is ignored
	lockFn   func()
	ttl      time.Duration
	// grants are short-lived tokens issued after the user re-enters their
	// password, keyed by SHA-256 of the token. They live only in memory and
	// are dropped on lock.
	grants map[string]sessionGrant
	// secretKey is kept while unlocked so a grant can be requested with the
	// password alone (e.g. from the onboarding page). Zeroed on lock.
	secretKey []byte
	// beforeAutoLock runs when the idle timer fires, while the key is still
	// held (e.g. to write a pending backup).
	beforeAutoLock func()
	// expiring is set once auto-lock has started: activity during
	// beforeAutoLock must not re-arm the timer of a session about to end.
	expiring bool
}

// sessionGrant is a password-gated grant. An empty scope means full
// session-level access; otherwise the grant is limited to scope and access,
// like a service token, but may reach personal fields.
type sessionGrant struct {
	expires time.Time
	scope   string
	access  string
}

// NewSession creates a session with the given vault key and auto-lock callback.
func NewSession(vaultKey []byte, lockFn func()) (*Session, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}

	s := &Session{
		token:  hex.EncodeToString(tokenBytes),
		lockFn: lockFn,
		ttl:    defaultAutoLockDuration,
	}
	// Copy vault key so caller can't mutate it
	s.vaultKey = make([]byte, len(vaultKey))
	copy(s.vaultKey, vaultKey)
	lockMemory(s.vaultKey)
	disableCoreDumps()

	s.armTimer()
	return s, nil
}

// armTimer starts a fresh idle timer; s.mu must be held (or s unpublished).
// Resetting the old one isn't enough: a firing may already be waiting for
// s.mu, and only its generation tells it that it is stale.
func (s *Session) armTimer() {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timerGen++
	gen := s.timerGen
	s.timer = time.AfterFunc(s.ttl, func() { s.autoLockGen(gen) })
}

// Token returns the session token string.
func (s *Session) Token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// HasKey reports whether the session still holds the vault key.
func (s *Session) HasKey() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vaultKey != nil
}

// VaultKey returns a copy of the vault key. Returns nil if session is destroyed.
// The caller clears the copy when done.
func (s *Session) VaultKey() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vaultKey == nil {
		return nil
	}
	cp := make([]byte, len(s.vaultKey))
	copy(cp, s.vaultKey)
	return cp
}

// ValidateToken checks the unlock token (constant-time) or an unexpired
// full-access grant. Scoped grants are checked with ScopedGrant.
func (s *Session) ValidateToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(s.token), []byte(token)) == 1 {
		return true
	}
	g, ok := s.grants[hashToken(token)]
	return ok && g.scope == "" && time.Now().Before(g.expires)
}

// ScopedGrant returns the scope and access of an unexpired scoped grant.
func (s *Session) ScopedGrant(token string) (scope, access string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" {
		return "", "", false
	}
	g, found := s.grants[hashToken(token)]
	if !found || g.scope == "" || !time.Now().Before(g.expires) {
		return "", "", false
	}
	return g.scope, g.access, true
}

// setSecretKey stores a copy of the secret key for password-only grants.
func (s *Session) setSecretKey(sk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secretKey = make([]byte, len(sk))
	copy(s.secretKey, sk)
	lockMemory(s.secretKey)
}

// SecretKey returns a copy of the secret key, or nil if destroyed or unset.
func (s *Session) SecretKey() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.secretKey == nil {
		return nil
	}
	cp := make([]byte, len(s.secretKey))
	copy(cp, s.secretKey)
	return cp
}

// AddGrant issues a short-lived token limited to scope and access, or a
// full session-level one when scope is empty. Returns ErrLocked if the
// session has been destroyed.
func (s *Session) AddGrant(ttl time.Duration, scope, access string) (string, time.Time, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(tokenBytes)
	expires := time.Now().Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" {
		return "", time.Time{}, ErrLocked
	}
	now := time.Now()
	for k, g := range s.grants {
		if now.After(g.expires) {
			delete(s.grants, k)
		}
	}
	if s.grants == nil {
		s.grants = make(map[string]sessionGrant)
	}
	s.grants[hashToken(token)] = sessionGrant{expires: expires, scope: scope, access: access}
	return token, expires, nil
}

// DropGrantsNaming removes the scoped grants whose scope names a member's
// slug, so a removed member's grants can't reach a later member with the
// same slug. Returns how many it removed.
func (s *Session) DropGrantsNaming(slug string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, g := range s.grants {
		if g.scope != "" && ScopeNamesMember(g.scope, slug) {
			delete(s.grants, k)
			n++
		}
	}
	return n
}

// Touch resets the auto-lock timer.
func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil && !s.expiring {
		s.armTimer()
	}
}

// Destroy zeroes the vault key and invalidates the session.
func (s *Session) Destroy() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zeroKey()
	s.token = ""
	s.grants = nil
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}

// setBeforeAutoLock sets a function auto-lock runs before zeroing the key.
func (s *Session) setBeforeAutoLock(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.beforeAutoLock = fn
}

func (s *Session) autoLock() { s.autoLockGen(0) }

// autoLockGen locks the session when timer generation gen fires (0: any).
func (s *Session) autoLockGen(gen uint64) {
	s.mu.Lock()
	if s.expiring || s.token == "" || (gen != 0 && gen != s.timerGen) {
		// Already locked, or a stale firing: nothing to do.
		s.mu.Unlock()
		return
	}
	s.expiring = true
	before := s.beforeAutoLock
	s.mu.Unlock()
	if before != nil {
		before()
	}

	s.mu.Lock()
	s.zeroKey()
	s.token = ""
	s.grants = nil
	s.timer = nil
	lockFn := s.lockFn
	s.mu.Unlock()

	if lockFn != nil {
		lockFn()
	}
}

func (s *Session) zeroKey() {
	unlockMemory(s.vaultKey)
	clear(s.vaultKey)
	s.vaultKey = nil
	unlockMemory(s.secretKey)
	clear(s.secretKey)
	s.secretKey = nil
}
