package vault

import (
	"strconv"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// Retention: how long a business keeps a person's data after the
// relationship ends (ruolo.data_fine), and a client's ID and procura scans
// after the last pratica naming them closes. Both depend on the business and
// on what the data is for, and no period has been checked against a source
// (docs/design/business-vaults.md, D8), so neither has a default: until the
// user sets one, 'pvault expiring' reports nothing about it. They are vault
// settings, not fields, so only the user's session can change them.

const (
	metaRetentionYears = "retention_years"
	metaScanDays       = "scan_days"
)

// MaxScanDays caps the period for scans after the last pratica closes.
const MaxScanDays = 36500

// MaxRetentionYears caps the retention period.
const MaxRetentionYears = 100

// Kinds of entries in Expiring besides a document or pratica deadline.
const (
	// ExpiryRetention: a person whose ruolo.data_fine plus the retention
	// period has passed or falls within the window. ExpiresOn is that date.
	ExpiryRetention = "retention"
	// ExpiryPraticheClosed: a client's ID or procura scans in a business
	// vault once every pratica naming them is closed, if the user set a
	// period for scans (ScanDays). ExpiresOn is the day the last of those
	// pratiche closed plus that period.
	ExpiryPraticheClosed = "pratiche_closed"
	// ExpiryFilesClosed: the same for a file in the file index (files.go)
	// that is linked to a client with a role naming an ID or procura
	// category (cie, passaporto, procura...). The vault doesn't hold it and
	// can't delete it: the user does, where it is. FileID and File say which.
	ExpiryFilesClosed = "files_closed"
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
	return v.SetRetention(&years, nil)
}

// SetScanDays sets the period for scans; -1 removes it.
func (v *Vault) SetScanDays(days int) error {
	return v.SetRetention(nil, &days)
}

// SetRetention sets the retention period in years (0 removes it) and the
// period for scans in days (-1 removes it); nil leaves one as it is.
// Both are checked before either changes.
func (v *Vault) SetRetention(years, scanDays *int) error {
	if years != nil && (*years < 0 || *years > MaxRetentionYears) {
		return &ValidationError{Field: "years", Msg: "must be between 1 and " + strconv.Itoa(MaxRetentionYears) + " (0 removes the setting)"}
	}
	if scanDays != nil && (*scanDays < -1 || *scanDays > MaxScanDays) {
		return &ValidationError{Field: "scan_days", Msg: "must be between 0 and " + strconv.Itoa(MaxScanDays) + " (-1 removes the setting)"}
	}
	if err := v.checkUnlocked(); err != nil {
		return err
	}
	if years != nil {
		value := ""
		if *years > 0 {
			value = strconv.Itoa(*years)
		}
		if err := v.db.SetMeta(metaRetentionYears, value); err != nil {
			return err
		}
		v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*:ruolo.data_fine", Action: "retention_set", Purpose: value})
	}
	if scanDays != nil {
		value := ""
		if *scanDays >= 0 {
			value = strconv.Itoa(*scanDays)
		}
		if err := v.db.SetMeta(metaScanDays, value); err != nil {
			return err
		}
		v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*:" + strings.Join(idScanCategories, ".documents,*:") + ".documents", Action: "scan_days_set", Purpose: value})
	}
	v.markChanged()
	return nil
}

// ScanDays returns how many days after the last pratica naming a client
// closes their ID and procura scans are reported, and whether it is set
// (0 is a period: right away).
func (v *Vault) ScanDays() (days int, set bool, err error) {
	s, err := v.db.GetMeta(metaScanDays)
	if err != nil || s == "" {
		return 0, false, err
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false, nil // a value this version can't read counts as unset
	}
	return n, true, nil
}

// retentionEnd returns the day the retention period after a relationship's
// end date runs out: the same day years later, or the month's last day when
// that day doesn't exist (29 February to 28 February, art. 2963 c.c.).
func retentionEnd(dataFine string, years int) (time.Time, bool) {
	d, err := NormalizeDate(dataFine)
	if err != nil {
		return time.Time{}, false
	}
	start, _ := time.Parse(dateLayout, d)
	end := start.AddDate(years, 0, 0)
	if end.Day() != start.Day() { // AddDate went on into March
		end = end.AddDate(0, 0, -end.Day())
	}
	return end, true
}

// isRuoloDataFine reports ruolo.data_fine (lower-case local ID).
func isRuoloDataFine(category, name string) bool {
	return category == "ruolo" && name == "data_fine"
}
