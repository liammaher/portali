package vault

import (
	"strconv"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// Retention: how long a business keeps a person's data after the
// relationship ends (ruolo.data_fine). The period depends on the business and
// on what the data is for, and no period has been checked against a source
// (docs/design/business-vaults.md, D8), so it has no default: until the user
// sets one, 'pvault expiring' reports nothing about it. It is a vault
// setting, not a field, so only the user's session can change it.

const metaRetentionYears = "retention_years"

// MaxRetentionYears caps the retention period.
const MaxRetentionYears = 100

// Kinds of entries in Expiring besides a document or pratica deadline.
const (
	// ExpiryRetention: a person whose ruolo.data_fine plus the retention
	// period has passed or falls within the window. ExpiresOn is that date.
	ExpiryRetention = "retention"
	// ExpiryPraticheClosed: a person's ID or procura scans in a business
	// vault once every pratica naming them is closed. ExpiresOn is today:
	// they are due now.
	ExpiryPraticheClosed = "pratiche_closed"
)

// idScanCategories are the categories whose scans a business keeps only
// while a pratica needs them: copies of ID documents, and the procura.
var idScanCategories = []string{"identity", "cie", "passaporto", "patente", "permesso_soggiorno", "tessera_sanitaria", "procura"}

// isIDScanCategory reports a category in idScanCategories, or a numbered
// copy of one (cie2, procura2).
func isIDScanCategory(category string) bool {
	c := schemaCategoryOf(category)
	if c == nil {
		return false
	}
	for _, name := range idScanCategories {
		if c.Name == name {
			return true
		}
	}
	return false
}

// RetentionYears returns the retention period in years, 0 if not set.
func (v *Vault) RetentionYears() (int, error) {
	s, err := v.db.GetMeta(metaRetentionYears)
	if err != nil || s == "" {
		return 0, err
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, nil // a value this version can't read counts as unset
	}
	return n, nil
}

// SetRetentionYears sets the retention period; 0 removes it.
func (v *Vault) SetRetentionYears(years int) error {
	if years < 0 || years > MaxRetentionYears {
		return &ValidationError{Field: "years", Msg: "must be between 1 and " + strconv.Itoa(MaxRetentionYears) + " (0 removes the setting)"}
	}
	if err := v.checkUnlocked(); err != nil {
		return err
	}
	value := ""
	if years > 0 {
		value = strconv.Itoa(years)
	}
	if err := v.db.SetMeta(metaRetentionYears, value); err != nil {
		return err
	}
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*:ruolo.data_fine", Action: "retention_set", Purpose: value})
	v.markChanged()
	return nil
}

// retentionEnd returns the day the retention period after a relationship's
// end date runs out.
func retentionEnd(dataFine string, years int) (time.Time, bool) {
	d, err := NormalizeDate(dataFine)
	if err != nil {
		return time.Time{}, false
	}
	end, _ := time.Parse(dateLayout, d)
	return end.AddDate(years, 0, 0), true
}

// isRuoloDataFine reports ruolo.data_fine (lower-case local ID).
func isRuoloDataFine(category, name string) bool {
	return strings.EqualFold(category, "ruolo") && strings.EqualFold(name, "data_fine")
}
