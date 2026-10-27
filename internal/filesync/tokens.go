package filesync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// SecretStore keeps a sign-in's tokens: the macOS Keychain in use, memory in tests.
type SecretStore interface {
	// Load returns "" with no error when there is nothing under service.
	Load(service string) (string, error)
	Save(service, secret string) error
	Delete(service string) error
}

// ServiceName is the Keychain service a file source's tokens are kept under.
func ServiceName(sourceID string) string { return "pvault-files-" + sourceID }

var reService = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]{0,99}$`)

// securityPath is run by absolute path, so nothing earlier in PATH sees a token.
const securityPath = "/usr/bin/security"

const securityItemNotFound = 44 // errSecItemNotFound

// KeychainStore is the login Keychain, through /usr/bin/security. Values are
// stored as unpadded base64url so they can't carry anything the tool would
// read as another command, and go to it on stdin, never on a command line.
type KeychainStore struct{}

func keychainAccount() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	if !reService.MatchString(u.Username) {
		return "", fmt.Errorf("unsupported user name %q for the Keychain", u.Username)
	}
	return u.Username, nil
}

func (KeychainStore) check(service string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("reading an account through its API keeps its sign-in in the macOS Keychain: this needs a Mac")
	}
	if !reService.MatchString(service) {
		return "", fmt.Errorf("invalid Keychain service %q", service)
	}
	if _, err := os.Stat(securityPath); err != nil {
		return "", fmt.Errorf("%s not found", securityPath)
	}
	return keychainAccount()
}

func (k KeychainStore) Load(service string) (string, error) {
	account, err := k.check(service)
	if err != nil {
		return "", err
	}
	out, err := exec.Command(securityPath, "find-generic-password", "-a", account, "-s", service, "-w").Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == securityItemNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return "", fmt.Errorf("the Keychain item %s isn't one pvault wrote", service)
	}
	return string(raw), nil
}

func (k KeychainStore) Save(service, secret string) error {
	account, err := k.check(service)
	if err != nil {
		return err
	}
	enc := base64.RawURLEncoding.EncodeToString([]byte(secret))
	add := exec.Command(securityPath, "-i")
	add.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -a %s -s %s -w %s\n", account, service, enc))
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("security: %v: %s", err, bytes.TrimSpace(out))
	}
	got, err := k.Load(service)
	if err != nil {
		return fmt.Errorf("could not read the sign-in back from the Keychain: %w", err)
	}
	if got != secret {
		return errors.New("the Keychain returned a different value than was stored")
	}
	return nil
}

func (k KeychainStore) Delete(service string) error {
	account, err := k.check(service)
	if err != nil {
		return err
	}
	err = exec.Command(securityPath, "delete-generic-password", "-a", account, "-s", service).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == securityItemNotFound {
		return nil
	}
	return err
}

// MemoryStore is a SecretStore in memory, for tests.
type MemoryStore struct {
	mu sync.Mutex
	M  map[string]string
}

func (m *MemoryStore) Load(service string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.M[service], nil
}

func (m *MemoryStore) Save(service, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.M == nil {
		m.M = map[string]string{}
	}
	m.M[service] = secret
	return nil
}

func (m *MemoryStore) Delete(service string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.M, service)
	return nil
}

// TokenSource hands out a valid access token for one source, refreshing it
// (and writing a rotated refresh token back) when it is about to end.
type TokenSource struct {
	Flow    *Flow
	Store   SecretStore
	Service string

	mu  sync.Mutex
	tok *Token
	now func() time.Time
}

// NewTokenSource reads the source's tokens. A source nobody signed in for (the
// Keychain item is gone) says so.
func NewTokenSource(flow *Flow, store SecretStore, service string) (*TokenSource, error) {
	raw, err := store.Load(service)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, &AuthError{Msg: "no sign-in saved for this source (it is not in the Keychain on this computer): connect it again"}
	}
	var tok Token
	if err := json.Unmarshal([]byte(raw), &tok); err != nil {
		return nil, &AuthError{Msg: "the saved sign-in is unreadable: connect the source again"}
	}
	return &TokenSource{Flow: flow, Store: store, Service: service, tok: &tok, now: time.Now}, nil
}

// NewTokenSourceFor is NewTokenSource for a sign-in just made, which the
// caller has saved (or will save) under service.
func NewTokenSourceFor(flow *Flow, store SecretStore, service string, tok *Token) *TokenSource {
	t := *tok
	return &TokenSource{Flow: flow, Store: store, Service: service, tok: &t, now: time.Now}
}

// SaveToken stores a fresh sign-in under service.
func SaveToken(store SecretStore, service string, tok *Token) error {
	raw, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return store.Save(service, string(raw))
}

// Access returns an access token with at least a minute left.
func (t *TokenSource) Access(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tok.AccessToken != "" && (t.tok.Expiry.IsZero() || t.tok.Expiry.After(t.now().Add(time.Minute))) {
		return t.tok.AccessToken, nil
	}
	return t.refreshLocked(ctx)
}

// Invalidate forgets the access token (a provider refused it): the next
// Access refreshes.
func (t *TokenSource) Invalidate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tok.AccessToken = ""
}

func (t *TokenSource) refreshLocked(ctx context.Context) (string, error) {
	fresh, err := t.Flow.Refresh(ctx, t.tok)
	if err != nil {
		return "", err
	}
	t.tok = fresh
	// A rotated refresh token is the only one that works from now on: if it
	// can't be saved the next run would be locked out, so say so.
	if err := SaveToken(t.Store, t.Service, fresh); err != nil {
		return "", fmt.Errorf("signed in again, but the new sign-in couldn't be saved: %w", err)
	}
	return fresh.AccessToken, nil
}
