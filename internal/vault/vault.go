package vault

import (
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lovincyrus/personal-vault/internal/crypto"
	"github.com/lovincyrus/personal-vault/internal/store"
)

var (
	ErrLocked          = errors.New("vault is locked")
	ErrAlreadyUnlocked = errors.New("vault is already unlocked")
	ErrNotInitialized  = errors.New("vault is not initialized")
	ErrAlreadyInit     = errors.New("vault is already initialized")
	ErrWrongPassword   = errors.New("wrong password or secret key")
	ErrInvalidTier     = errors.New("invalid sensitivity tier: must be public, standard, sensitive, or critical")
	ErrAmbiguousToken  = errors.New("token prefix matches more than one service token; use a longer prefix")
)

var validTiers = map[string]bool{
	"public": true, "standard": true, "sensitive": true, "critical": true, TierPersonal: true,
}

// tierRank orders sensitivity tiers from least to most sensitive.
var tierRank = map[string]int{
	"public": 0, "standard": 1, "sensitive": 2, "critical": 3, TierPersonal: 4,
}

// TierAllows reports whether a token limited to maxTier may access a field
// with the given tier. Unknown tiers are treated as critical.
func TierAllows(maxTier, fieldTier string) bool {
	maxRank, ok := tierRank[maxTier]
	if !ok {
		return false
	}
	fieldRank, ok := tierRank[fieldTier]
	if !ok {
		fieldRank = tierRank["critical"]
	}
	return fieldRank <= maxRank
}

const (
	// DefaultServiceTokenTTL is used when no TTL is requested.
	DefaultServiceTokenTTL = 24 * time.Hour
	// MaxServiceTokenTTL caps how long an agent token can live.
	MaxServiceTokenTTL = 90 * 24 * time.Hour
	// DefaultServiceTokenMaxSensitivity keeps critical fields away from agents
	// unless the user opts in when creating the token.
	DefaultServiceTokenMaxSensitivity = "sensitive"
	// minTokenPrefixLen is the shortest hash prefix accepted for revocation.
	minTokenPrefixLen = 8
	// MaxSessionGrantTTL caps password-gated session grants.
	MaxSessionGrantTTL = time.Hour
	// MaxScopedGrantTTL caps scoped grants, which back a single approval.
	MaxScopedGrantTTL = 10 * time.Minute
)

// Token access levels. Read-only is the default: agents must be explicitly
// allowed to add data (append) or to overwrite and delete it (write).
const (
	AccessRead   = "read"   // read fields and documents only
	AccessAppend = "append" // also add new fields and documents; no overwrite or delete
	AccessWrite  = "write"  // also overwrite and delete
)

var validAccess = map[string]bool{AccessRead: true, AccessAppend: true, AccessWrite: true}

// ErrInvalidAccess is returned for an unknown token access level.
var ErrInvalidAccess = errors.New("invalid access: must be read, append, or write")

// Vault is the main entry point for vault operations.
type Vault struct {
	mu      sync.RWMutex
	db      *store.DB
	session *Session
	dir     string // ~/.pvault
	salt    []byte // loaded on unlock, used for HKDF subkey derivation
	backup  backupState
}

// Open opens an existing vault database.
func Open(dir string) (*Vault, error) {
	dbPath := filepath.Join(dir, "vault.db")
	db, err := store.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &Vault{db: db, dir: dir}, nil
}

// CanInit reports why Init would refuse dir, so the CLI can say so before
// asking for a password.
func CanInit(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "vault.db")); err == nil {
		return ErrAlreadyInit
	}
	if skPath := filepath.Join(dir, "secret.key"); fileExists(skPath) {
		return errSecretKeyExists(skPath)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func errSecretKeyExists(path string) error {
	return fmt.Errorf("%s already exists, maybe the key for an earlier vault's backups — move it somewhere safe, then run init again", path)
}

// Init creates a new vault: generates salt, secret key, and stores verification ciphertext.
func Init(dir, password string) (secretKey string, err error) {
	if err := CanInit(dir); err != nil {
		return "", err
	}
	// Create directory
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create vault dir: %w", err)
	}
	dbPath := filepath.Join(dir, "vault.db")
	skPath := filepath.Join(dir, "secret.key")

	// Generate salt
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	// Generate secret key
	sk, err := crypto.GenerateSecretKey()
	if err != nil {
		return "", fmt.Errorf("generate secret key: %w", err)
	}

	// Write the secret key first, never over an existing one: a key left
	// from an earlier vault may be the only way to open its backups.
	skFile, err := os.OpenFile(skPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return "", errSecretKeyExists(skPath)
	}
	if err != nil {
		return "", fmt.Errorf("write secret key: %w", err)
	}
	_, err = skFile.WriteString(hex.EncodeToString(sk) + "\n")
	if cerr := skFile.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(skPath)
		return "", fmt.Errorf("write secret key: %w", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		os.Remove(skPath)
		return "", fmt.Errorf("create database: %w", err)
	}
	// A half-made vault would count as initialized without a usable key, so
	// any failure from here removes both files.
	defer func() {
		db.Close()
		if err != nil {
			for _, f := range []string{dbPath, dbPath + "-wal", dbPath + "-shm", skPath} {
				os.Remove(f)
			}
		}
	}()

	// Store salt and secret key hash in DB
	if err := db.SetMeta("salt", base64.StdEncoding.EncodeToString(salt)); err != nil {
		return "", err
	}
	if err := db.SetMeta("secret_key_hash", hex.EncodeToString(crypto.HashSecretKey(sk))); err != nil {
		return "", err
	}

	// Derive vault key and create verification ciphertext
	vaultKey := crypto.DeriveVaultKey([]byte(password), sk, salt)
	verifyPlaintext := []byte("personal-vault-verification")
	verifyCipher, err := crypto.EncryptToBase64(vaultKey, verifyPlaintext)
	if err != nil {
		return "", fmt.Errorf("create verification: %w", err)
	}
	if err := db.SetMeta("verification", verifyCipher); err != nil {
		return "", err
	}

	// Zero vault key
	clear(vaultKey)

	return hex.EncodeToString(sk), nil
}

// parseSecretKey decodes a secret key, also as copied by hand: any case,
// with spaces, dashes or line breaks.
func parseSecretKey(s string) ([]byte, error) {
	s = strings.NewReplacer(" ", "", "-", "", "\t", "", "\r", "", "\n", "").Replace(s)
	return hex.DecodeString(s)
}

// Unlock derives the vault key and creates a session.
func (v *Vault) Unlock(password string, secretKeyHex string) (token string, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.session != nil {
		return "", ErrAlreadyUnlocked
	}

	sk, err := parseSecretKey(secretKeyHex)
	if err != nil {
		return "", ErrWrongPassword
	}
	vaultKey, salt, err := v.verifyCredentials(password, sk)
	if err != nil {
		return "", err
	}

	// Store salt for HKDF subkey derivation
	v.salt = salt

	// Create session. Auto-lock only clears it if it is still the current
	// session, never one created by a later unlock.
	var session *Session
	session, err = NewSession(vaultKey, func() {
		v.mu.Lock()
		if v.session == session {
			v.session = nil
		}
		v.mu.Unlock()
	})
	if err != nil {
		return "", err
	}
	session.setSecretKey(sk)
	session.setBeforeAutoLock(v.flushBackup)
	v.session = session

	// Zero local copies of the vault key and secret key
	clear(vaultKey)
	clear(sk)

	// Log access
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*", Action: "unlock"})

	return session.Token(), nil
}

// CheckCredentials reports whether the password and secret key unlock this
// vault (ErrWrongPassword if not) without creating a session.
func (v *Vault) CheckCredentials(password, secretKeyHex string) error {
	sk, err := parseSecretKey(secretKeyHex)
	if err != nil {
		return ErrWrongPassword
	}
	vaultKey, _, err := v.verifyCredentials(password, sk)
	if err != nil {
		return err
	}
	clear(vaultKey)
	return nil
}

// verifyCredentials checks the password and secret key and returns the
// derived vault key and salt. The caller must zero the key.
func (v *Vault) verifyCredentials(password string, sk []byte) (vaultKey, salt []byte, err error) {
	init, err := v.db.IsInitialized()
	if err != nil {
		return nil, nil, err
	}
	if !init {
		return nil, nil, ErrNotInitialized
	}

	// Load salt
	saltB64, err := v.db.GetMeta("salt")
	if err != nil {
		return nil, nil, err
	}
	salt, err = base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode salt: %w", err)
	}

	// Verify secret key hash
	storedHash, err := v.db.GetMeta("secret_key_hash")
	if err != nil {
		return nil, nil, err
	}
	actualHash := hex.EncodeToString(crypto.HashSecretKey(sk))
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(actualHash)) != 1 {
		return nil, nil, ErrWrongPassword
	}

	// Derive vault key
	vaultKey = crypto.DeriveVaultKey([]byte(password), sk, salt)

	// Verify with stored ciphertext
	verifyCipher, err := v.db.GetMeta("verification")
	if err != nil {
		return nil, nil, err
	}
	plaintext, err := crypto.DecryptFromBase64(vaultKey, verifyCipher)
	if err != nil || string(plaintext) != "personal-vault-verification" {
		clear(vaultKey)
		return nil, nil, ErrWrongPassword
	}
	return vaultKey, salt, nil
}

// CreateSessionGrant re-checks the user's password (and secret key, if given;
// otherwise the one held since unlock) against an unlocked vault and issues a
// short-lived session-level token. The CLI and the onboarding page
// authenticate this way, so no reusable token is stored on disk or passed on
// a command line where an agent with shell access could read it.
func (v *Vault) CreateSessionGrant(password, secretKeyHex string, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 || ttl > MaxSessionGrantTTL {
		return "", time.Time{}, &ValidationError{Field: "ttl", Msg: fmt.Sprintf("must be greater than 0 and at most %s", MaxSessionGrantTTL)}
	}
	return v.issueGrant(password, secretKeyHex, ttl, "", "",
		store.AuditEntry{Consumer: "vault", Scope: "*", Action: "session_grant", Purpose: "ttl: " + ttl.String()})
}

// CreateScopedGrant is CreateSessionGrant limited to scope and access, for
// one approved request (e.g. a Touch ID prompt that named exactly these
// fields). Unlike a service token it can reach personal fields, because the
// user approved it in person; it cannot do session-only operations.
func (v *Vault) CreateScopedGrant(password, secretKeyHex string, ttl time.Duration, scope, access string) (string, time.Time, error) {
	if ttl <= 0 || ttl > MaxScopedGrantTTL {
		return "", time.Time{}, &ValidationError{Field: "ttl", Msg: fmt.Sprintf("must be greater than 0 and at most %s", MaxScopedGrantTTL)}
	}
	if err := ValidateScope(scope); err != nil {
		return "", time.Time{}, &ValidationError{Field: "scope", Msg: err.Error()}
	}
	if access == "" {
		access = AccessRead
	}
	if !validAccess[access] {
		return "", time.Time{}, ErrInvalidAccess
	}
	return v.issueGrant(password, secretKeyHex, ttl, scope, access,
		store.AuditEntry{Consumer: "vault", Scope: scope, Action: "scoped_grant", Purpose: "access: " + access + ", ttl: " + ttl.String()})
}

// issueGrant checks the credentials, then issues a grant (full access when
// scope is empty), resets the auto-lock timer and records entry.
func (v *Vault) issueGrant(password, secretKeyHex string, ttl time.Duration, scope, access string, entry store.AuditEntry) (string, time.Time, error) {
	session, err := v.checkGrantCredentials(password, secretKeyHex)
	if err != nil {
		return "", time.Time{}, err
	}
	token, expires, err := session.AddGrant(ttl, scope, access)
	if err != nil {
		return "", time.Time{}, err
	}
	session.Touch()
	v.db.LogAccess(entry)
	return token, expires, nil
}

// checkGrantCredentials verifies the password and secret key (or the one
// held since unlock) and returns the live session.
func (v *Vault) checkGrantCredentials(password, secretKeyHex string) (*Session, error) {
	v.mu.RLock()
	session := v.session
	v.mu.RUnlock()
	if session == nil {
		return nil, ErrLocked
	}

	var sk []byte
	if strings.TrimSpace(secretKeyHex) == "" {
		if sk = session.SecretKey(); sk == nil {
			return nil, ErrLocked
		}
	} else {
		var err error
		if sk, err = parseSecretKey(secretKeyHex); err != nil {
			return nil, ErrWrongPassword
		}
	}
	vaultKey, _, err := v.verifyCredentials(password, sk)
	clear(sk)
	if err != nil {
		return nil, err
	}
	clear(vaultKey)
	return session, nil
}

// Lock destroys the session and zeroes the vault key, first writing any
// automatic backup still waiting and letting one in progress finish (Close
// closes the database right after).
func (v *Vault) Lock() {
	v.backup.mu.Lock()
	defer v.backup.mu.Unlock()
	v.flushBackupLocked()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.session != nil {
		v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*", Action: "lock"})
		v.session.Destroy()
		v.session = nil
	}
}

// Status returns the current vault status.
func (v *Vault) Status() (*VaultStatus, error) {
	init, err := v.db.IsInitialized()
	if err != nil {
		return nil, err
	}

	status := &VaultStatus{
		Initialized: init,
		Locked:      true,
	}

	v.mu.RLock()
	if v.session != nil {
		status.Locked = false
	}
	v.mu.RUnlock()

	if init {
		// Status is public, so personal fields and documents are left out of
		// every count: agents must not even learn that they exist.
		count, _ := v.db.FieldCount(TierPersonal)
		status.FieldCount = count
		cats, _ := v.db.CategoryCounts(TierPersonal)
		status.Categories = cats
		status.Documents, _ = v.db.DocumentCount(TierPersonal)
		status.Backup = v.backupStatus()
	}

	return status, nil
}

// Set encrypts and stores a field value.
func (v *Vault) Set(id, value, sensitivity string) error {
	if err := ValidateFieldID(id); err != nil {
		return err
	}
	value, err := NormalizeAndValidate(id, value)
	if err != nil {
		return err
	}

	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return err
	}

	parts := strings.SplitN(id, ".", 2)
	category, fieldName := parts[0], parts[1]

	// Derive category subkey
	subkey, err := crypto.DeriveSubkey(vaultKey, v.salt, category)
	if err != nil {
		return fmt.Errorf("derive subkey: %w", err)
	}

	// Encrypt
	encrypted, err := crypto.EncryptToBase64(subkey, []byte(value))
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	// With no explicit tier, keep an existing field's tier (so overwriting a
	// value never silently downgrades it) and use the schema default for new
	// fields. The tier read here is only for validation: the store keeps
	// whatever tier the row has when it writes (KeepTier).
	keepTier := sensitivity == ""
	if keepTier {
		existing, err := v.db.GetField(id)
		if err != nil {
			return err
		}
		if existing != nil {
			sensitivity = existing.Sensitivity
		} else {
			sensitivity = DefaultSensitivity(id)
		}
	}
	if !validTiers[sensitivity] {
		return ErrInvalidTier
	}
	if IsCredentialField(id) && sensitivity != TierPersonal {
		return errCredentialTier(id)
	}

	err = v.db.SetField(store.Field{
		ID:          id,
		Category:    category,
		FieldName:   fieldName,
		Value:       encrypted,
		Sensitivity: sensitivity,
		UpdatedAt:   time.Now(),
		KeepTier:    keepTier,
	})
	if err != nil {
		return err
	}

	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: id, Action: "write"})
	v.markChanged()
	return nil
}

// Get decrypts and returns a field value.
func (v *Vault) Get(id string) (*FieldInfo, error) {
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}

	f, err := v.db.GetField(id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, nil
	}

	subkey, err := crypto.DeriveSubkey(vaultKey, v.salt, f.Category)
	if err != nil {
		return nil, err
	}

	plaintext, err := crypto.DecryptFromBase64(subkey, f.Value)
	if err != nil {
		return nil, fmt.Errorf("decrypt field %s: %w", id, err)
	}

	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: id, Action: "read"})

	return &FieldInfo{
		ID:          f.ID,
		Category:    f.Category,
		FieldName:   f.FieldName,
		Value:       string(plaintext),
		Sensitivity: f.Sensitivity,
		UpdatedAt:   f.UpdatedAt,
		Version:     f.Version,
	}, nil
}

// FieldMeta returns a field's metadata without decrypting its value.
// Returns nil if the field does not exist.
func (v *Vault) FieldMeta(id string) (*FieldInfo, error) {
	if _, err := v.requireUnlocked(); err != nil {
		return nil, err
	}
	f, err := v.db.GetField(id)
	if err != nil || f == nil {
		return nil, err
	}
	return &FieldInfo{
		ID:          f.ID,
		Category:    f.Category,
		FieldName:   f.FieldName,
		Sensitivity: f.Sensitivity,
		UpdatedAt:   f.UpdatedAt,
		Version:     f.Version,
	}, nil
}

// List returns all field metadata (no values).
func (v *Vault) List() ([]FieldInfo, error) {
	if _, err := v.requireUnlocked(); err != nil {
		return nil, err
	}

	fields, err := v.db.ListFields()
	if err != nil {
		return nil, err
	}

	result := make([]FieldInfo, len(fields))
	for i, f := range fields {
		result[i] = FieldInfo{
			ID:          f.ID,
			Category:    f.Category,
			FieldName:   f.FieldName,
			Sensitivity: f.Sensitivity,
			UpdatedAt:   f.UpdatedAt,
			Version:     f.Version,
		}
	}
	return result, nil
}

// ListByCategory returns field metadata for a category (no values).
func (v *Vault) ListByCategory(category string) ([]FieldInfo, error) {
	if _, err := v.requireUnlocked(); err != nil {
		return nil, err
	}

	fields, err := v.db.ListFieldsByCategory(category)
	if err != nil {
		return nil, err
	}

	result := make([]FieldInfo, len(fields))
	for i, f := range fields {
		result[i] = FieldInfo{
			ID:          f.ID,
			Category:    f.Category,
			FieldName:   f.FieldName,
			Sensitivity: f.Sensitivity,
			UpdatedAt:   f.UpdatedAt,
			Version:     f.Version,
		}
	}
	return result, nil
}

// FieldFilter reports whether a caller may receive a field. Fields it rejects
// are skipped before decryption. A nil filter allows every field.
type FieldFilter func(id, sensitivity string) bool

func (allow FieldFilter) allows(f store.Field) bool {
	return allow == nil || allow(f.ID, f.Sensitivity)
}

// GetByCategory returns the decrypted fields of a category that allow accepts.
func (v *Vault) GetByCategory(category string, allow FieldFilter) ([]FieldInfo, error) {
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}

	fields, err := v.db.GetFieldsByCategory(category)
	if err != nil {
		return nil, err
	}

	subkey, err := crypto.DeriveSubkey(vaultKey, v.salt, category)
	if err != nil {
		return nil, err
	}

	result := make([]FieldInfo, 0, len(fields))
	for _, f := range fields {
		if !allow.allows(f) {
			continue
		}
		plaintext, err := crypto.DecryptFromBase64(subkey, f.Value)
		if err != nil {
			return nil, fmt.Errorf("decrypt %s: %w", f.ID, err)
		}
		result = append(result, FieldInfo{
			ID:          f.ID,
			Category:    f.Category,
			FieldName:   f.FieldName,
			Value:       string(plaintext),
			Sensitivity: f.Sensitivity,
			UpdatedAt:   f.UpdatedAt,
			Version:     f.Version,
		})
	}

	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: category + ".*", Action: "read"})
	return result, nil
}

// GetContext returns the decrypted fields allow accepts, grouped by category.
func (v *Vault) GetContext(allow FieldFilter) (*ContextBundle, error) {
	bundle, err := v.decryptFields(allow)
	if err != nil {
		return nil, err
	}
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*", Action: "context"})
	return bundle, nil
}

// decryptFields decrypts the fields allow accepts, grouped by category,
// without writing an audit entry (callers log what they actually did).
func (v *Vault) decryptFields(allow FieldFilter) (*ContextBundle, error) {
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}

	fields, err := v.db.GetAllFields()
	if err != nil {
		return nil, err
	}

	bundle := &ContextBundle{Categories: make(map[string][]FieldInfo)}
	subkeys := make(map[string][]byte)

	for _, f := range fields {
		if !allow.allows(f) {
			continue
		}
		sk, ok := subkeys[f.Category]
		if !ok {
			sk, err = crypto.DeriveSubkey(vaultKey, v.salt, f.Category)
			if err != nil {
				return nil, err
			}
			subkeys[f.Category] = sk
		}

		plaintext, err := crypto.DecryptFromBase64(sk, f.Value)
		if err != nil {
			return nil, fmt.Errorf("decrypt %s: %w", f.ID, err)
		}

		bundle.Categories[f.Category] = append(bundle.Categories[f.Category], FieldInfo{
			ID:          f.ID,
			Category:    f.Category,
			FieldName:   f.FieldName,
			Value:       string(plaintext),
			Sensitivity: f.Sensitivity,
			UpdatedAt:   f.UpdatedAt,
			Version:     f.Version,
		})
	}

	return bundle, nil
}

// Delete removes a field.
func (v *Vault) Delete(id string) error {
	if _, err := v.requireUnlocked(); err != nil {
		return err
	}

	if err := v.db.DeleteField(id); err != nil {
		return err
	}

	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: id, Action: "delete"})
	v.markChanged()
	return nil
}

// SetSensitivity updates a field's sensitivity tier.
func (v *Vault) SetSensitivity(id, tier string) error {
	if _, err := v.requireUnlocked(); err != nil {
		return err
	}
	if !validTiers[tier] {
		return ErrInvalidTier
	}
	if IsCredentialField(id) && tier != TierPersonal {
		return errCredentialTier(id)
	}
	if err := v.db.SetSensitivity(id, tier); err != nil {
		return err
	}
	v.markChanged()
	return nil
}

// AuditLog returns recent audit entries.
func (v *Vault) AuditLog(limit int) ([]store.AuditEntry, error) {
	return v.db.GetAuditLog(limit)
}

// ValidateToken checks a session token.
func (v *Vault) ValidateToken(token string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.session == nil {
		return false
	}
	return v.session.ValidateToken(token)
}

// ScopedGrant returns the scope and access of a valid scoped grant.
func (v *Vault) ScopedGrant(token string) (scope, access string, ok bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.session == nil {
		return "", "", false
	}
	return v.session.ScopedGrant(token)
}

// hashToken returns the hex-encoded SHA-256 hash of a token. Service tokens
// and session grants are stored and looked up by this hash.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// CreateServiceToken generates a service token for a consumer. The scope must
// be explicit, ttl must be between zero and MaxServiceTokenTTL, and the token
// can only read fields up to maxSensitivity (default "sensitive").
// The raw token is returned to the caller; only the SHA-256 hash is stored.
func (v *Vault) CreateServiceToken(consumer, scope string, ttl time.Duration, maxSensitivity, access string) (string, error) {
	if _, err := v.requireUnlocked(); err != nil {
		return "", err
	}
	if err := ValidateScope(scope); err != nil {
		return "", &ValidationError{Field: "scope", Msg: err.Error()}
	}
	if ttl <= 0 || ttl > MaxServiceTokenTTL {
		return "", &ValidationError{Field: "ttl", Msg: fmt.Sprintf("must be greater than 0 and at most %s", MaxServiceTokenTTL)}
	}
	if maxSensitivity == "" {
		maxSensitivity = DefaultServiceTokenMaxSensitivity
	}
	if !validTiers[maxSensitivity] {
		return "", ErrInvalidTier
	}
	if maxSensitivity == TierPersonal {
		return "", &ValidationError{Field: "max_sensitivity", Msg: "personal fields are never available to agents; the highest cap is critical"}
	}
	if access == "" {
		access = AccessRead
	}
	if !validAccess[access] {
		return "", ErrInvalidAccess
	}

	tokenBytes := make([]byte, 32)
	if _, err := crand.Read(tokenBytes); err != nil {
		return "", err
	}
	tokenStr := hex.EncodeToString(tokenBytes)

	t := store.Token{
		TokenStr:       hashToken(tokenStr),
		Consumer:       consumer,
		Scope:          scope,
		ExpiresAt:      time.Now().Add(ttl),
		Usage:          "service",
		CreatedAt:      time.Now(),
		MaxSensitivity: maxSensitivity,
		Access:         access,
	}
	if err := v.db.CreateToken(t); err != nil {
		return "", err
	}

	v.db.LogAccess(store.AuditEntry{
		Consumer: "vault",
		Scope:    scope,
		Action:   "create_service_token",
		Purpose:  "consumer: " + consumer + ", max_sensitivity: " + maxSensitivity + ", access: " + access,
	})
	v.markChanged()

	return tokenStr, nil
}

// ValidateServiceToken checks a service token by hashing it and looking up the hash.
func (v *Vault) ValidateServiceToken(token string) (*store.Token, bool) {
	t, err := v.db.GetToken(hashToken(token))
	if err != nil || t == nil {
		return nil, false
	}
	if t.Usage != "service" {
		return nil, false
	}
	return t, true
}

// ListServiceTokens returns all service tokens.
func (v *Vault) ListServiceTokens() ([]store.Token, error) {
	if _, err := v.requireUnlocked(); err != nil {
		return nil, err
	}
	return v.db.ListTokensByUsage("service")
}

// RevokeServiceToken removes a service token. ref is either the raw token or
// a prefix (at least 8 hex characters) of its stored hash, as shown by
// ListServiceTokens. A prefix must match exactly one token.
func (v *Vault) RevokeServiceToken(ref string) (int64, error) {
	if _, err := v.requireUnlocked(); err != nil {
		return 0, err
	}
	hash, err := v.resolveServiceToken(ref)
	if err != nil || hash == "" {
		return 0, err
	}
	n, err := v.db.DeleteToken(hash)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		v.db.LogAccess(store.AuditEntry{
			Consumer: "vault",
			Scope:    "*",
			Action:   "revoke_service_token",
		})
		v.markChanged()
	}
	return n, nil
}

// resolveServiceToken maps a raw token or hash prefix to the stored hash.
// Returns "" if nothing matches.
func (v *Vault) resolveServiceToken(ref string) (string, error) {
	tokens, err := v.db.ListTokensByUsage("service")
	if err != nil {
		return "", err
	}
	fullHash := hashToken(ref)
	for _, t := range tokens {
		if t.TokenStr == fullHash {
			return fullHash, nil
		}
	}

	prefix := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(ref), "..."))
	if len(prefix) < minTokenPrefixLen || strings.Trim(prefix, "0123456789abcdef") != "" {
		return "", nil
	}
	var match string
	for _, t := range tokens {
		if strings.HasPrefix(t.TokenStr, prefix) {
			if match != "" {
				return "", ErrAmbiguousToken
			}
			match = t.TokenStr
		}
	}
	return match, nil
}

// TouchSession resets the auto-lock timer. Only call this for activity by the
// user (session token), so agent traffic cannot keep the vault unlocked.
func (v *Vault) TouchSession() {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.session != nil {
		v.session.Touch()
	}
}

// Close closes the database.
func (v *Vault) Close() error {
	v.Lock()
	return v.db.Close()
}

// LogAccess writes an entry to the audit log.
func (v *Vault) LogAccess(entry store.AuditEntry) {
	v.db.LogAccess(entry)
}

func (v *Vault) requireUnlocked() ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.session == nil {
		return nil, ErrLocked
	}
	key := v.session.VaultKey()
	if key == nil {
		return nil, ErrLocked
	}
	return key, nil
}
