package vault

import (
	"sort"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// ExpiryInfo describes a document expiry date found in the vault.
type ExpiryInfo struct {
	FieldID   string `json:"field_id"`
	Document  string `json:"document"`   // category, e.g. permesso_soggiorno
	ExpiresOn string `json:"expires_on"` // YYYY-MM-DD
	DaysLeft  int    `json:"days_left"`  // negative once expired
	Expired   bool   `json:"expired"`
}

// Expiring returns expiry dates (fields named *data_scadenza) that have
// passed or fall within the given window, soonest first. Only fields allow
// accepts are decrypted. Values that are not valid dates (e.g. written by an
// older version) are skipped.
func (v *Vault) Expiring(within time.Duration, allow FieldFilter, now time.Time) ([]ExpiryInfo, error) {
	ctx, err := v.decryptFields(func(id, sensitivity string) bool {
		parts := strings.SplitN(id, ".", 2)
		return len(parts) == 2 && IsExpiryField(parts[1]) && (allow == nil || allow(id, sensitivity))
	})
	if err != nil {
		return nil, err
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	cutoff := today.Add(within)
	result := []ExpiryInfo{}
	for _, fields := range ctx.Categories {
		for _, f := range fields {
			normalized, err := NormalizeDate(f.Value)
			if err != nil {
				continue
			}
			expires, _ := time.Parse(dateLayout, normalized)
			if expires.After(cutoff) {
				continue
			}
			days := int(expires.Sub(today).Hours() / 24)
			result = append(result, ExpiryInfo{
				FieldID:   f.ID,
				Document:  f.Category,
				ExpiresOn: normalized,
				DaysLeft:  days,
				Expired:   days < 0,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ExpiresOn < result[j].ExpiresOn })
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*.data_scadenza", Action: "expiring"})
	return result, nil
}
