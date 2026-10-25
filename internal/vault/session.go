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
	lockFn   func()
	ttl      time.Duration
	// grants are short-lived tokens issued after the user re-enters their
	// password, keyed by SHA-256 of the token. They live only in memory and
	// are dropped on lock.
	grants map[string]sessionGrant
	// secretKey is kept while unlocked so a grant can be requested with the
	// password alone (e.g. from the onboarding page). Zeroed on lock.
	secretKey []byte
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

	s.timer = time.AfterFunc(s.ttl, s.autoLock)
	return s, nil
}

// Token returns the session token string.
func (s *Session) Token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// VaultKey returns a copy of the vault key. Returns nil if session is destroyed.
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

// AddGrant issues a short-lived session-level token. Returns ErrLocked if the
// session has been destroyed.
func (s *Session) AddGrant(ttl time.Duration) (string, time.Time, error) {
	return s.addGrant(ttl, "", "")
}

// AddScopedGrant issues a short-lived token limited to scope and access.
func (s *Session) AddScopedGrant(ttl time.Duration, scope, access string) (string, time.Time, error) {
	return s.addGrant(ttl, scope, access)
}

func (s *Session) addGrant(ttl time.Duration, scope, access string) (string, time.Time, error) {
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

// Touch resets the auto-lock timer.
func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Reset(s.ttl)
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

func (s *Session) autoLock() {
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
	for i := range s.vaultKey {
		s.vaultKey[i] = 0
	}
	s.vaultKey = nil
	unlockMemory(s.secretKey)
	for i := range s.secretKey {
		s.secretKey[i] = 0
	}
	s.secretKey = nil
}
