package vault

import (
	"crypto/rand"
	"crypto/sha256"
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
	// grants are short-lived session-level tokens issued after the user
	// re-enters their password, keyed by SHA-256 of the token. They live only
	// in memory and are dropped on lock.
	grants map[string]time.Time
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

// ValidateToken checks the unlock token (constant-time) or an unexpired grant.
func (s *Session) ValidateToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(s.token), []byte(token)) == 1 {
		return true
	}
	expires, ok := s.grants[hashGrant(token)]
	return ok && time.Now().Before(expires)
}

// AddGrant issues a short-lived session-level token. Returns "" if the
// session has been destroyed.
func (s *Session) AddGrant(ttl time.Duration) (string, time.Time, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(tokenBytes)
	expires := time.Now().Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" {
		return "", time.Time{}, nil
	}
	now := time.Now()
	for k, exp := range s.grants {
		if now.After(exp) {
			delete(s.grants, k)
		}
	}
	if s.grants == nil {
		s.grants = make(map[string]time.Time)
	}
	s.grants[hashGrant(token)] = expires
	return token, expires, nil
}

func hashGrant(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
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
}
