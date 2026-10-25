package vault

import (
	"testing"
	"time"
)

func TestMemProtect_NoPanic(t *testing.T) {
	// Verify lockMemory/unlockMemory/disableCoreDumps don't panic.
	// These are best-effort and may silently fail without CAP_IPC_LOCK,
	// but they must never crash the process.
	b := make([]byte, 32)
	lockMemory(b)
	unlockMemory(b)
	disableCoreDumps()
}

func TestSession_MemProtect_Integration(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")

	s, err := NewSession(key, func() {})
	if err != nil {
		t.Fatal(err)
	}

	// Key should be accessible
	got := s.VaultKey()
	if got == nil {
		t.Fatal("expected non-nil vault key")
	}
	if string(got) != string(key) {
		t.Fatal("vault key mismatch")
	}

	// Destroy should zero and unlock without panic
	s.Destroy()

	if s.VaultKey() != nil {
		t.Fatal("expected nil vault key after destroy")
	}
	if s.ValidateToken(s.Token()) {
		t.Fatal("expected invalid token after destroy")
	}
}

func TestSession_AutoLock_MemProtect(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	locked := make(chan struct{})

	s, err := NewSession(key, func() { close(locked) })
	if err != nil {
		t.Fatal(err)
	}

	// Shorten TTL to trigger auto-lock quickly
	s.mu.Lock()
	s.ttl = 1
	s.timer.Reset(1)
	s.mu.Unlock()

	<-locked

	// After auto-lock, key should be zeroed (unlockMemory called in zeroKey)
	if s.VaultKey() != nil {
		t.Fatal("expected nil vault key after auto-lock")
	}
}

// A user request during auto-lock's backup must not re-arm the old session's
// timer, and a stale auto-lock must never drop a later session.
func TestStaleAutoLockKeepsNewSession(t *testing.T) {
	v, sk := tmpVault(t)
	v.mu.RLock()
	old := v.session
	v.mu.RUnlock()
	old.setBeforeAutoLock(old.Touch) // activity while the backup runs
	old.autoLock()
	old.mu.Lock()
	rearmed := old.timer != nil
	old.mu.Unlock()
	if rearmed {
		t.Fatal("touch during auto-lock re-armed the timer")
	}

	if _, err := v.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	v.mu.RLock()
	current := v.session
	v.mu.RUnlock()
	old.autoLock() // a stale firing of the old timer
	old.lockFn()   // even if it got as far as the vault's callback
	v.mu.RLock()
	still := v.session
	v.mu.RUnlock()
	if still != current || current.VaultKey() == nil {
		t.Fatal("stale auto-lock dropped the current session")
	}
}

// A timer that already fired but is still waiting for the lock when the
// user's activity re-arms it must not lock the vault.
func TestTouchCancelsPendingAutoLock(t *testing.T) {
	locked := make(chan struct{})
	s, err := NewSession(make([]byte, 32), func() { close(locked) })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Destroy()
	s.mu.Lock()
	s.ttl = 10 * time.Millisecond
	s.armTimer()
	time.Sleep(50 * time.Millisecond) // fired; its auto-lock waits for s.mu
	s.ttl = time.Hour
	s.armTimer() // what Touch does
	s.mu.Unlock()
	select {
	case <-locked:
		t.Fatal("stale timer locked the vault after activity")
	case <-time.After(200 * time.Millisecond):
	}
}
