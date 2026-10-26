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
	Document  string `json:"document"`         // category without the person, e.g. permesso_soggiorno, or "pratica <slug>"
	Person    string `json:"member,omitempty"` // whose it is: a person's slug, empty for the owner (JSON key from before the rename)
	ExpiresOn string `json:"expires_on"`       // YYYY-MM-DD
	DaysLeft  int    `json:"days_left"`        // negative once expired
	Expired   bool   `json:"expired"`
}

// Expiring returns expiry dates (fields named *data_scadenza) that have
// passed or fall within the given window, soonest first. Only fields allow
// accepts are decrypted. Values that are not valid dates (e.g. written by an
// older version) are skipped, and so are the deadlines of closed pratiche.
func (v *Vault) Expiring(within time.Duration, allow FieldFilter, now time.Time) ([]ExpiryInfo, error) {
	ctx, err := v.decryptFields(func(id, sensitivity string) bool {
		parts := strings.SplitN(id, ".", 2)
		if len(parts) != 2 || (allow != nil && !allow(id, sensitivity)) {
			return false
		}
		_, campo, _ := praticaField(parts[0], parts[1])
		return IsExpiryField(parts[1]) || campo == "stato" || campo == "persona"
	})
	if err != nil {
		return nil, err
	}

	// Pratiche whose stato ends them, and whom each one is for, by slug.
	closed := map[string]bool{}
	persona := map[string]string{}
	for _, fields := range ctx.Categories {
		for _, f := range fields {
			slug, campo, _ := praticaField(f.Category, f.FieldName)
			switch {
			case campo == "stato" && isClosedStato(f.Value):
				closed[slug] = true
			case campo == "persona" && f.Value != OwnerRef:
				persona[slug] = f.Value
			}
		}
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	cutoff := today.Add(within)
	result := []ExpiryInfo{}
	for _, fields := range ctx.Categories {
		for _, f := range fields {
			slug, _, isPratica := praticaField(f.Category, f.FieldName)
			if !IsExpiryField(f.FieldName) || (isPratica && closed[slug]) {
				continue
			}
			normalized, err := NormalizeDate(f.Value)
			if err != nil {
				continue
			}
			expires, _ := time.Parse(dateLayout, normalized)
			if expires.After(cutoff) {
				continue
			}
			days := int(expires.Sub(today).Hours() / 24)
			person, document := SplitPerson(f.Category)
			if isPratica {
				document = "pratica " + slug // a pratica's deadline
				person = persona[slug]
			}
			result = append(result, ExpiryInfo{
				FieldID:   f.ID,
				Document:  document,
				Person:    person,
				ExpiresOn: normalized,
				DaysLeft:  days,
				Expired:   days < 0,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ExpiresOn < result[j].ExpiresOn })
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*.data_scadenza,pratiche.*_stato", Action: "expiring"})
	return result, nil
}
