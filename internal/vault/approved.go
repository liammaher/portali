package vault

import (
	"fmt"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// Grants the user approved in person on this computer, in a prompt the vault
// server showed itself (Windows Hello): the same grants as with the password,
// without it. The server already holds the vault key, so trusting its own
// prompt's answer gives nothing to an attacker inside it that it didn't have.
// The prompt is worded from the request (GrantPrompt, SignPrompt) and the
// grant issued is exactly that request.

const metaHello = "hello"

// HelloEnabled reports whether Windows Hello may approve grants on this vault.
func (v *Vault) HelloEnabled() (bool, error) {
	s, err := v.db.GetMeta(metaHello)
	return s == "on", err
}

// SetHelloEnabled turns Windows Hello approvals on or off; the API lets only
// the session do it.
func (v *Vault) SetHelloEnabled(on bool) error {
	if err := v.checkUnlocked(); err != nil {
		return err
	}
	value, action := "", "hello_disabled"
	if on {
		value, action = "on", "hello_enabled"
	}
	if err := v.db.SetMeta(metaHello, value); err != nil {
		return err
	}
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*", Action: action})
	v.markChanged()
	return nil
}

// PromptLabel names this vault in the prompts it shows.
func (v *Vault) PromptLabel() string {
	return PromptVaultLabel(v.dir)
}

func (v *Vault) unlockedSession() (*Session, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.session == nil || !v.session.HasKey() {
		return nil, ErrLocked
	}
	return v.session, nil
}

// CreateApprovedGrant is CreateSessionGrant (empty scope) or CreateScopedGrant
// for a request the user approved in person; via says how ("hello").
func (v *Vault) CreateApprovedGrant(ttl time.Duration, scope, access, via string) (string, time.Time, error) {
	access, err := CheckGrantRequest(ttl, scope, access)
	if err != nil {
		return "", time.Time{}, err
	}
	session, err := v.unlockedSession()
	if err != nil {
		return "", time.Time{}, err
	}
	entry := grantAudit(ttl, scope, access)
	entry.Purpose += ", via: " + via
	return v.issueGrant(session, ttl, scope, access, entry)
}

// CreateApprovedSignGrant is CreateSignGrant for a request the user approved
// in person; via says how ("hello").
func (v *Vault) CreateApprovedSignGrant(id, via string) (string, time.Time, error) {
	session, err := v.unlockedSession()
	if err != nil {
		return "", time.Time{}, err
	}
	info, err := v.SigningStatus(id, "")
	if err != nil {
		return "", time.Time{}, err
	}
	if info.Status != SigningPending {
		return "", time.Time{}, ErrSigningNotPending
	}
	token, expires, err := session.AddSignGrant(SignGrantTTL, id)
	if err != nil {
		return "", time.Time{}, err
	}
	session.Touch()
	v.auditSigning("vault", "sign_grant_"+via, *info)
	return token, expires, nil
}

// AuditApprovalRefused records a request the user refused in person.
func (v *Vault) AuditApprovalRefused(via, scope, what string) {
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: scope, Action: "approval_refused", Purpose: fmt.Sprintf("via: %s; %s", via, what)})
}
