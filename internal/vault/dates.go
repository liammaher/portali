package vault

import (
	"errors"
	"strings"
	"time"
)

// dateLayout is the stored form of every date field.
const dateLayout = "2006-01-02"

// IsDateField reports whether a field name holds a calendar date by
// convention: any underscore-separated word "data" or "date", e.g.
// data_scadenza, membro1_data_nascita, date_of_birth.
func IsDateField(name string) bool {
	for _, w := range strings.Split(strings.ToLower(name), "_") {
		if w == "data" || w == "date" {
			return true
		}
	}
	return false
}

// IsExpiryField reports whether a field records a document's expiry date.
func IsExpiryField(name string) bool {
	return hasFieldSuffix(strings.ToLower(name), "data_scadenza")
}

// NormalizeDate accepts YYYY-MM-DD or the Italian day-first forms
// DD/MM/YYYY, DD-MM-YYYY and DD.MM.YYYY, rejects impossible dates such as
// 31/02/2026, and returns YYYY-MM-DD.
func NormalizeDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	layouts := []string{dateLayout, "02/01/2006", "02-01-2006", "02.01.2006", "2/1/2006"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(dateLayout), nil
		}
	}
	return "", errors.New("date must be YYYY-MM-DD or DD/MM/YYYY and a real calendar date")
}
