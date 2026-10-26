package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
)

const testPassword = "test-password-123"

func tmpVault(t *testing.T) (*Vault, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".pvault")
	sk, err := Init(dir, testPassword)
	if err != nil {
		t.Fatal(err)
	}

	v, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	token, err := v.Unlock(testPassword, sk)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	return v, sk
}

func TestInit_CreatesFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pvault")
	sk, err := Init(dir, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if sk == "" {
		t.Fatal("expected non-empty secret key")
	}

	// Check files exist
	if _, err := os.Stat(filepath.Join(dir, "vault.db")); err != nil {
		t.Fatalf("vault.db not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secret.key")); err != nil {
		t.Fatalf("secret.key not created: %v", err)
	}
}

func TestInit_AlreadyInitialized(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pvault")
	Init(dir, testPassword)
	_, err := Init(dir, testPassword)
	if err != ErrAlreadyInit {
		t.Fatalf("expected ErrAlreadyInit, got %v", err)
	}
}

func TestUnlock_WrongPassword(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pvault")
	sk, _ := Init(dir, testPassword)

	v, _ := Open(dir)
	defer v.Close()

	_, err := v.Unlock("wrong-password", sk)
	if err != ErrWrongPassword {
		t.Fatalf("expected ErrWrongPassword, got %v", err)
	}
}

func TestUnlock_WrongSecretKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pvault")
	Init(dir, testPassword)

	v, _ := Open(dir)
	defer v.Close()

	_, err := v.Unlock(testPassword, "deadbeefdeadbeefdeadbeefdeadbeef")
	if err != ErrWrongPassword {
		t.Fatalf("expected ErrWrongPassword, got %v", err)
	}
}

func TestUnlock_AlreadyUnlocked(t *testing.T) {
	v, _ := tmpVault(t)

	_, err := v.Unlock(testPassword, "doesntmatter")
	if err != ErrAlreadyUnlocked {
		t.Fatalf("expected ErrAlreadyUnlocked, got %v", err)
	}
}

func TestLock(t *testing.T) {
	v, _ := tmpVault(t)
	v.Lock()

	_, err := v.Get("anything")
	if err != ErrLocked {
		t.Fatalf("expected ErrLocked after lock, got %v", err)
	}
}

func TestSetGet_Roundtrip(t *testing.T) {
	v, _ := tmpVault(t)

	if err := v.Set("identity.full_name", "Jane Smith", ""); err != nil {
		t.Fatal(err)
	}

	f, err := v.Get("identity.full_name")
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatal("field not found")
	}
	if f.Value != "Jane Smith" {
		t.Fatalf("expected 'Jane Smith', got %q", f.Value)
	}
	if f.Category != "identity" {
		t.Fatalf("expected category 'identity', got %q", f.Category)
	}
}

func TestSet_InvalidID(t *testing.T) {
	v, _ := tmpVault(t)
	err := v.Set("noperiod", "value", "")
	if err == nil {
		t.Fatal("expected error for invalid field ID")
	}
}

func TestGet_NotFound(t *testing.T) {
	v, _ := tmpVault(t)
	f, err := v.Get("nonexistent.field")
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatal("expected nil for nonexistent field")
	}
}

func TestList(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.name", "Test", "")
	v.Set("financial.income", "100k", "sensitive")

	fields, err := v.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	// List should not include values
	for _, f := range fields {
		if f.Value != "" {
			t.Fatal("List should not include decrypted values")
		}
	}
}

func TestListByCategory(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.name", "Test", "")
	v.Set("identity.dob", "1990-01-01", "")
	v.Set("financial.income", "100k", "")

	fields, err := v.ListByCategory("identity")
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 identity fields, got %d", len(fields))
	}
}

func TestGetByCategory(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.name", "Jane", "")
	v.Set("identity.dob", "1990-01-01", "")

	fields, err := v.GetByCategory("identity", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	// Should include values
	for _, f := range fields {
		if f.Value == "" {
			t.Fatal("GetByCategory should include decrypted values")
		}
	}
}

func TestGetContext(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.name", "Jane", "")
	v.Set("identity.dob", "1990-01-01", "")
	v.Set("financial.income", "100k", "")

	ctx, err := v.GetContext(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(ctx.Categories))
	}
	if len(ctx.Categories["identity"]) != 2 {
		t.Fatalf("expected 2 identity fields, got %d", len(ctx.Categories["identity"]))
	}
	if len(ctx.Categories["financial"]) != 1 {
		t.Fatalf("expected 1 financial field, got %d", len(ctx.Categories["financial"]))
	}
}

func TestDelete(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.name", "Jane", "")
	v.Delete("identity.name")

	f, _ := v.Get("identity.name")
	if f != nil {
		t.Fatal("field should be deleted")
	}
}

func TestStatus(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.name", "Jane", "")
	v.Set("financial.income", "100k", "")

	status, err := v.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Locked {
		t.Fatal("should not be locked")
	}
	if !status.Initialized {
		t.Fatal("should be initialized")
	}
	if status.FieldCount != 2 {
		t.Fatalf("expected 2 fields, got %d", status.FieldCount)
	}
}

func TestValidateToken(t *testing.T) {
	v, _ := tmpVault(t)
	token := v.session.Token()

	if !v.ValidateToken(token) {
		t.Fatal("valid token should pass")
	}
	if v.ValidateToken("wrong-token") {
		t.Fatal("wrong token should fail")
	}
}

func TestAutoLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pvault")
	sk, _ := Init(dir, testPassword)
	v, _ := Open(dir)
	defer v.Close()

	v.Unlock(testPassword, sk)

	// Override TTL to very short
	v.session.mu.Lock()
	v.session.ttl = 50 * time.Millisecond
	v.session.timer.Reset(50 * time.Millisecond)
	v.session.mu.Unlock()

	time.Sleep(150 * time.Millisecond)

	_, err := v.Get("anything")
	if err != ErrLocked {
		t.Fatalf("expected ErrLocked after auto-lock, got %v", err)
	}
}

func TestAuditLog(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.name", "Jane", "")
	v.Get("identity.name")

	entries, err := v.AuditLog(50)
	if err != nil {
		t.Fatal(err)
	}
	// At minimum: unlock + set write + set log + get read + get log
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 audit entries, got %d", len(entries))
	}
}

func TestSetSensitivity(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.ssn", "123-45-6789", "standard")
	v.SetSensitivity("identity.ssn", "critical")

	f, _ := v.Get("identity.ssn")
	if f.Sensitivity != "critical" {
		t.Fatalf("expected critical, got %s", f.Sensitivity)
	}
}

func TestCreateServiceToken(t *testing.T) {
	v, _ := tmpVault(t)

	token, err := v.CreateServiceToken("life", "*", 24*time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected non-empty service token")
	}
	if len(token) != 64 { // 32 bytes hex-encoded
		t.Fatalf("expected 64-char hex token, got %d chars", len(token))
	}
}

func TestValidateServiceToken(t *testing.T) {
	v, _ := tmpVault(t)

	token, _ := v.CreateServiceToken("life", "*", 24*time.Hour, "", "")

	svcToken, ok := v.ValidateServiceToken(token)
	if !ok {
		t.Fatal("expected valid service token")
	}
	if svcToken.Consumer != "life" {
		t.Fatalf("expected consumer 'life', got %q", svcToken.Consumer)
	}
	if svcToken.Usage != "service" {
		t.Fatalf("expected usage 'service', got %q", svcToken.Usage)
	}
}

func TestValidateServiceToken_Invalid(t *testing.T) {
	v, _ := tmpVault(t)

	_, ok := v.ValidateServiceToken("nonexistent-token")
	if ok {
		t.Fatal("expected invalid for nonexistent token")
	}
}

func TestValidateServiceToken_SessionTokenNotAccepted(t *testing.T) {
	v, _ := tmpVault(t)
	sessionToken := v.session.Token()

	_, ok := v.ValidateServiceToken(sessionToken)
	if ok {
		t.Fatal("session token should not validate as service token")
	}
}

func TestListServiceTokens(t *testing.T) {
	v, _ := tmpVault(t)
	v.CreateServiceToken("life", "*", 24*time.Hour, "", "")
	v.CreateServiceToken("other-app", "identity.*", 24*time.Hour, "", "")

	tokens, err := v.ListServiceTokens()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 service tokens, got %d", len(tokens))
	}
}

func TestRevokeServiceToken(t *testing.T) {
	v, _ := tmpVault(t)
	token, _ := v.CreateServiceToken("life", "*", 24*time.Hour, "", "")

	n, err := v.RevokeServiceToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 revoked, got %d", n)
	}

	_, ok := v.ValidateServiceToken(token)
	if ok {
		t.Fatal("revoked token should not validate")
	}
}

func TestServiceToken_RequiresUnlocked(t *testing.T) {
	v, _ := tmpVault(t)
	v.Lock()

	_, err := v.CreateServiceToken("life", "*", 24*time.Hour, "", "")
	if err != ErrLocked {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
}

func TestAutoLock_NotResetByFieldAccess(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.name", "Jane", "")

	v.session.mu.Lock()
	v.session.ttl = 100 * time.Millisecond
	v.session.timer.Reset(100 * time.Millisecond)
	v.session.mu.Unlock()

	// Continuous reads (e.g. by an agent) must not keep the vault unlocked.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		v.Get("identity.name")
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := v.Get("identity.name"); err != ErrLocked {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
}

func TestCreateServiceToken_Validation(t *testing.T) {
	v, _ := tmpVault(t)
	if _, err := v.CreateServiceToken("a", "", time.Hour, "", ""); err == nil {
		t.Error("empty scope should be rejected")
	}
	if _, err := v.CreateServiceToken("a", "identity.*,", time.Hour, "", ""); err == nil {
		t.Error("trailing empty pattern should be rejected")
	}
	if _, err := v.CreateServiceToken("a", "*", 0, "", ""); err == nil {
		t.Error("zero ttl should be rejected")
	}
	if _, err := v.CreateServiceToken("a", "*", MaxServiceTokenTTL+time.Hour, "", ""); err == nil {
		t.Error("ttl above max should be rejected")
	}
	token, err := v.CreateServiceToken("a", "identity.*,residenza.comune", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	svc, ok := v.ValidateServiceToken(token)
	if !ok || svc.MaxSensitivity != "sensitive" {
		t.Fatalf("expected default max_sensitivity sensitive, got %+v", svc)
	}
}

func TestRevokeServiceToken_ByHashPrefix(t *testing.T) {
	v, _ := tmpVault(t)
	token, _ := v.CreateServiceToken("life", "*", time.Hour, "", "")
	prefix := hashToken(token)[:12]

	n, err := v.RevokeServiceToken(prefix + "...")
	if err != nil || n != 1 {
		t.Fatalf("revoke by prefix: n=%d err=%v", n, err)
	}
	if _, ok := v.ValidateServiceToken(token); ok {
		t.Fatal("revoked token should not validate")
	}
	if n, _ := v.RevokeServiceToken("zz"); n != 0 {
		t.Fatal("non-matching ref should revoke nothing")
	}
}

func TestTierAllows(t *testing.T) {
	tests := []struct {
		max, field string
		want       bool
	}{
		{"critical", "critical", true},
		{"sensitive", "critical", false},
		{"sensitive", "sensitive", true},
		{"standard", "sensitive", false},
		{"public", "public", true},
		{"sensitive", "bogus", false}, // unknown field tier treated as critical
		{"bogus", "public", false},    // unknown cap allows nothing
	}
	for _, tt := range tests {
		if got := TierAllows(tt.max, tt.field); got != tt.want {
			t.Errorf("TierAllows(%q, %q) = %v, want %v", tt.max, tt.field, got, tt.want)
		}
	}
}

func TestCreateSessionGrant(t *testing.T) {
	v, sk := tmpVault(t)

	token, expires, err := v.CreateSessionGrant(testPassword, sk, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !v.ValidateToken(token) {
		t.Fatal("grant should validate as a session token")
	}
	if time.Until(expires) > time.Minute {
		t.Fatalf("expiry too far out: %v", expires)
	}
	if _, ok := v.ValidateServiceToken(token); ok {
		t.Fatal("grant must not validate as a service token")
	}

	if _, _, err := v.CreateSessionGrant("wrong-password", sk, time.Minute); err != ErrWrongPassword {
		t.Fatalf("wrong password: got %v", err)
	}
	if _, _, err := v.CreateSessionGrant(testPassword, "00", time.Minute); err != ErrWrongPassword {
		t.Fatalf("wrong secret key: got %v", err)
	}
	if _, _, err := v.CreateSessionGrant(testPassword, sk, 2*time.Hour); err == nil {
		t.Fatal("ttl above max should be rejected")
	}
}

func TestSessionGrant_Expires(t *testing.T) {
	v, sk := tmpVault(t)
	token, _, err := v.CreateSessionGrant(testPassword, sk, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if v.ValidateToken(token) {
		t.Fatal("expired grant should not validate")
	}
}

func TestSessionGrant_ClearedOnLock(t *testing.T) {
	v, sk := tmpVault(t)
	token, _, _ := v.CreateSessionGrant(testPassword, sk, time.Minute)
	v.Lock()
	if _, _, err := v.CreateSessionGrant(testPassword, sk, time.Minute); err != ErrLocked {
		t.Fatalf("grant on locked vault: got %v", err)
	}
	if _, err := v.Unlock(testPassword, sk); err != nil {
		t.Fatal(err)
	}
	if v.ValidateToken(token) {
		t.Fatal("grant from before lock must not survive unlock")
	}
}

func TestSet_OverwriteKeepsTier(t *testing.T) {
	v, _ := tmpVault(t)
	if err := v.Set("identity.codice_fiscale", "RSSMRA85T10A562S", ""); err != nil {
		t.Fatal(err)
	}
	f, _ := v.Get("identity.codice_fiscale")
	if f.Sensitivity != "sensitive" {
		t.Fatalf("new field should use schema default, got %s", f.Sensitivity)
	}
	v.SetSensitivity("identity.codice_fiscale", "critical")
	if err := v.Set("identity.codice_fiscale", "CCCFBA85D03L219P", ""); err != nil {
		t.Fatal(err)
	}
	f, _ = v.Get("identity.codice_fiscale")
	if f.Sensitivity != "critical" || f.Value != "CCCFBA85D03L219P" {
		t.Fatalf("overwrite without tier: got %s / %s, want critical / new value", f.Sensitivity, f.Value)
	}
	// An explicit tier still applies.
	v.Set("identity.codice_fiscale", "CCCFBA85D03L219P", "standard")
	f, _ = v.Get("identity.codice_fiscale")
	if f.Sensitivity != "standard" {
		t.Fatalf("explicit tier: got %s", f.Sensitivity)
	}
}

func TestCreateSessionGrant_PasswordOnly(t *testing.T) {
	v, _ := tmpVault(t)
	token, _, err := v.CreateSessionGrant(testPassword, "", time.Minute)
	if err != nil {
		t.Fatalf("password-only grant: %v", err)
	}
	if !v.ValidateToken(token) {
		t.Fatal("password-only grant should validate")
	}
	if _, _, err := v.CreateSessionGrant("wrong", "", time.Minute); err != ErrWrongPassword {
		t.Fatalf("wrong password: got %v", err)
	}
	v.Lock()
	if _, _, err := v.CreateSessionGrant(testPassword, "", time.Minute); err != ErrLocked {
		t.Fatalf("after lock: got %v", err)
	}
}

func TestCheckCredentials(t *testing.T) {
	v, sk := tmpVault(t)
	if err := v.CheckCredentials(testPassword, sk); err != nil {
		t.Fatalf("correct credentials: %v", err)
	}
	if err := v.CheckCredentials("nope", sk); err != ErrWrongPassword {
		t.Fatalf("wrong password: %v", err)
	}
	if err := v.CheckCredentials(testPassword, "zz"); err != ErrWrongPassword {
		t.Fatalf("malformed secret key: %v", err)
	}
}

func TestGetContext_FilterSkipsFields(t *testing.T) {
	v, _ := tmpVault(t)
	v.Set("identity.full_name", "Mario", "")
	v.Set("payment.card_number", "4111", "")
	var seen []string
	ctx, err := v.GetContext(func(id, tier string) bool {
		seen = append(seen, id)
		return tier != "critical"
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ctx.Categories["payment"]; ok {
		t.Fatal("filtered category returned")
	}
	if len(seen) != 2 {
		t.Fatalf("filter should see every field once, saw %v", seen)
	}
	fields, _ := v.GetByCategory("payment", func(id, tier string) bool { return false })
	if len(fields) != 0 {
		t.Fatalf("GetByCategory filter ignored: %v", fields)
	}
}

func TestCreateServiceToken_Access(t *testing.T) {
	v, _ := tmpVault(t)
	token, err := v.CreateServiceToken("a", "*", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if svc, _ := v.ValidateServiceToken(token); svc.Access != AccessRead {
		t.Fatalf("default access = %q, want read", svc.Access)
	}
	for _, access := range []string{AccessAppend, AccessWrite} {
		tok, err := v.CreateServiceToken("a", "*", time.Hour, "", access)
		if err != nil {
			t.Fatal(err)
		}
		if svc, _ := v.ValidateServiceToken(tok); svc.Access != access {
			t.Fatalf("access = %q, want %q", svc.Access, access)
		}
	}
	if _, err := v.CreateServiceToken("a", "*", time.Hour, "", "admin"); err != ErrInvalidAccess {
		t.Fatalf("invalid access: got %v", err)
	}
}

func TestCreateScopedGrant(t *testing.T) {
	v, sk := tmpVault(t)

	token, _, err := v.CreateScopedGrant(testPassword, sk, time.Minute, "identity.codice_fiscale", AccessRead)
	if err != nil {
		t.Fatal(err)
	}
	if v.ValidateToken(token) {
		t.Fatal("scoped grant must not validate as a full session token")
	}
	scope, access, ok := v.ScopedGrant(token)
	if !ok || scope != "identity.codice_fiscale" || access != AccessRead {
		t.Fatalf("ScopedGrant = %q, %q, %v", scope, access, ok)
	}
	full, _, _ := v.CreateSessionGrant(testPassword, sk, time.Minute)
	if _, _, ok := v.ScopedGrant(full); ok {
		t.Fatal("full grant must not look scoped")
	}

	if _, _, err := v.CreateScopedGrant("wrong", sk, time.Minute, "identity.*", ""); err != ErrWrongPassword {
		t.Fatalf("wrong password: got %v", err)
	}
	if _, _, err := v.CreateScopedGrant(testPassword, sk, time.Minute, "identity", ""); err == nil {
		t.Fatal("malformed scope should be rejected")
	}
	if _, _, err := v.CreateScopedGrant(testPassword, sk, time.Minute, "identity.*", "admin"); err != ErrInvalidAccess {
		t.Fatalf("bad access: got %v", err)
	}
	if _, _, err := v.CreateScopedGrant(testPassword, sk, MaxScopedGrantTTL+time.Second, "identity.*", ""); err == nil {
		t.Fatal("ttl above max should be rejected")
	}
	_, access, _ = v.ScopedGrant(mustScoped(t, v, sk, "identity.*", ""))
	if access != AccessRead {
		t.Fatalf("default access = %q, want read", access)
	}

	v.Lock()
	if _, _, ok := v.ScopedGrant(token); ok {
		t.Fatal("scoped grant must not survive lock")
	}
}

func TestScopedGrant_Expires(t *testing.T) {
	v, sk := tmpVault(t)
	token, _, err := v.CreateScopedGrant(testPassword, sk, 50*time.Millisecond, "identity.*", AccessRead)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, _, ok := v.ScopedGrant(token); ok {
		t.Fatal("expired scoped grant should not validate")
	}
}

func mustScoped(t *testing.T, v *Vault, sk, scope, access string) string {
	t.Helper()
	token, _, err := v.CreateScopedGrant(testPassword, sk, time.Minute, scope, access)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// A write without a tier must not undo a tier change that lands between its
// read of the old tier and its write.
func TestSetKeepsConcurrentTierRaise(t *testing.T) {
	v, _ := tmpVault(t)
	id := "notes.salary"
	for i := 0; i < 100; i++ {
		if err := v.Set(id, "100", "standard"); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := v.Set(id, "200", ""); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := v.SetSensitivity(id, "critical"); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		if m, _ := v.FieldMeta(id); m.Sensitivity != "critical" {
			t.Fatalf("run %d: tier is %q after a concurrent raise to critical", i, m.Sensitivity)
		}
	}
}

// A secret.key left from an earlier vault may be the only key to its
// backups, so init never replaces it.
func TestInitKeepsExistingSecretKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pvault")
	if _, err := Init(dir, testPassword); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "secret.key")
	before, _ := os.ReadFile(keyPath)
	for _, f := range []string{"vault.db", "vault.db-wal", "vault.db-shm"} {
		os.Remove(filepath.Join(dir, f))
	}
	if _, err := Init(dir, "other-password"); err == nil {
		t.Fatal("second init over an existing secret.key succeeded")
	}
	if after, _ := os.ReadFile(keyPath); !bytes.Equal(before, after) {
		t.Error("init replaced the existing secret.key")
	}
	if _, err := os.Stat(filepath.Join(dir, "vault.db")); !os.IsNotExist(err) {
		t.Errorf("refused init left a vault.db behind (stat: %v)", err)
	}
}

func TestInitCreatesOwnerOnlyDatabase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pvault")
	if _, err := Init(dir, testPassword); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"vault.db", "secret.key"} {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("%s has mode %#o, want 0600", f, info.Mode().Perm())
		}
	}
}

// Changing or deleting a field that isn't there is an error, not a silent
// success the user would take for done.
func TestMissingFieldErrors(t *testing.T) {
	v, _ := tmpVault(t)
	if err := v.SetSensitivity("notes.pin", "critical"); err != ErrFieldNotFound {
		t.Errorf("SetSensitivity on a missing field: %v", err)
	}
	if err := v.Delete("notes.pin"); err != ErrFieldNotFound {
		t.Errorf("Delete of a missing field: %v", err)
	}
}

// A checked write or delete happens only if the field is still as checked.
func TestCheckedWritesRefuseChangedFields(t *testing.T) {
	v, _ := tmpVault(t)
	if err := v.SetChecked("notes.a", "agent", "", nil, nil); err != nil {
		t.Fatalf("checked create: %v", err)
	}
	if err := v.SetChecked("notes.a", "again", "", nil, nil); err != ErrFieldChanged {
		t.Errorf("create over an existing field: %v", err)
	}
	seen, _ := v.FieldMeta("notes.a")
	if err := v.SetSensitivity("notes.a", "critical"); err != nil {
		t.Fatal(err)
	}
	if err := v.SetChecked("notes.a", "agent", "", seen, nil); err != ErrFieldChanged {
		t.Errorf("write after a tier change: %v", err)
	}
	if err := v.DeleteChecked("notes.a", seen); err != ErrFieldChanged {
		t.Errorf("delete after a tier change: %v", err)
	}
	if f, _ := v.Get("notes.a"); f == nil || f.Value != "agent" || f.Sensitivity != "critical" {
		t.Errorf("field = %+v", f)
	}
}

func TestListServiceTokensLeavesOutExpired(t *testing.T) {
	v, _ := tmpVault(t)
	if _, err := v.CreateServiceToken("short", "notes.*", 50*time.Millisecond, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := v.CreateServiceToken("long", "notes.*", time.Hour, "", ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	tokens, err := v.ListServiceTokens()
	if err != nil || len(tokens) != 1 || tokens[0].Consumer != "long" {
		t.Fatalf("tokens = %+v, %v", tokens, err)
	}
}

// GetIf checks the row it decrypts, so a field that turned personal after the
// caller's own tier check is not returned.
func TestGetIf_FilterSeesDecryptedRow(t *testing.T) {
	v, _ := tmpVault(t)
	if err := v.Set("cie.pin", "12345678", ""); err != nil {
		t.Fatal(err)
	}
	belowPersonal := func(id, sensitivity string) bool { return TierAllows("critical", sensitivity) }
	f, err := v.GetIf("cie.pin", belowPersonal)
	if err != nil || f != nil {
		t.Fatalf("GetIf returned %v, %v; want the personal field hidden", f, err)
	}
	if f, err := v.GetIf("cie.pin", nil); err != nil || f == nil || f.Value != "12345678" {
		t.Fatalf("GetIf with no filter = %v, %v", f, err)
	}
}

func TestOpen_DoesNotCreateDatabase(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir); err != ErrNotInitialized {
		t.Fatalf("Open on an empty dir = %v, want ErrNotInitialized", err)
	}
	if err := CanInit(dir); err != nil {
		t.Fatalf("CanInit after Open = %v, want nil", err)
	}
}

func TestInit_FailedDatabaseLeavesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pvault")
	if err := os.MkdirAll(filepath.Join(dir, "vault.db-shm"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir, testPassword); err == nil {
		t.Fatal("Init succeeded despite the blocked -shm file")
	}
	os.Remove(filepath.Join(dir, "vault.db-shm"))
	if err := CanInit(dir); err != nil {
		t.Fatalf("CanInit after a failed Init = %v, want nil", err)
	}
	if _, err := Init(dir, testPassword); err != nil {
		t.Fatalf("Init after cleanup: %v", err)
	}
}

// Every caller clears its copy of the vault key when done, so the copy must
// never be the session's own key: clearing it would wipe the vault key.
func TestVaultKeyCopiesClearedSafely(t *testing.T) {
	v, _ := tmpVault(t)
	k, err := v.requireUnlocked()
	if err != nil {
		t.Fatal(err)
	}
	clear(k)
	if again, _ := v.requireUnlocked(); bytes.Equal(again, make([]byte, len(again))) {
		t.Fatal("clearing a copy zeroed the session's vault key")
	}
	for i := 0; i < 3; i++ {
		if err := v.Set("identity.full_name", "Mario Rossi", ""); err != nil {
			t.Fatal(err)
		}
		if f, err := v.Get("identity.full_name"); err != nil || f.Value != "Mario Rossi" {
			t.Fatalf("round %d: %v, %v", i, f, err)
		}
	}
	v.Lock()
	if err := v.checkUnlocked(); err != ErrLocked {
		t.Fatalf("checkUnlocked after lock = %v", err)
	}
}

// An init stopped before its last write (Ctrl-C during Argon2, power loss)
// leaves a vault.db without verification: init and open say so and how to
// start again, instead of "already initialized" and "wrong password".
func TestInterruptedInitIsReported(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pvault")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMeta("salt", "c2FsdA==") // what an older init wrote before verification
	db.Close()
	os.WriteFile(filepath.Join(dir, "secret.key"), []byte("00\n"), 0600)

	if err := CanInit(dir); !errors.Is(err, ErrInitInterrupted) || !strings.Contains(err.Error(), "delete "+dir) {
		t.Errorf("CanInit = %v, want ErrInitInterrupted naming the folder", err)
	}
	if _, err := Init(dir, testPassword); !errors.Is(err, ErrInitInterrupted) {
		t.Errorf("Init = %v, want ErrInitInterrupted", err)
	}
	if _, err := Open(dir); !errors.Is(err, ErrInitInterrupted) {
		t.Errorf("Open = %v, want ErrInitInterrupted", err)
	}

	// A finished init is "already initialized", as before.
	done := filepath.Join(t.TempDir(), ".pvault")
	if _, err := Init(done, testPassword); err != nil {
		t.Fatal(err)
	}
	if err := CanInit(done); !errors.Is(err, ErrAlreadyInit) {
		t.Errorf("CanInit on a finished vault = %v", err)
	}
}
