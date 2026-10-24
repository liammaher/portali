package vault

import (
	"errors"
	"strings"
	"time"
)

// dateLayout is the stored form of every date field.
const dateLayout = "2006-01-02"

// IsDateField reports whether a field name holds a calendar date by
// convention: Italian data_<x> (data_scadenza, membro1_data_nascita) or
// English date_of_<x> / <x>_date (date_of_birth, start_date). Names such as
// date_format are not dates. Hyphens count as underscores.
func IsDateField(name string) bool {
	words := fieldWords(name)
	for i, w := range words {
		last := i == len(words)-1
		switch {
		case w == "data" && !last:
			return true
		case w == "date" && last && i > 0:
			return true
		case w == "date" && !last && words[i+1] == "of":
			return true
		}
	}
	return false
}

// IsExpiryField reports whether a field records a document's expiry date.
func IsExpiryField(name string) bool {
	return hasFieldSuffix(strings.Join(fieldWords(name), "_"), "data_scadenza")
}

// fieldWords splits a lower-cased field name on underscores and hyphens.
func fieldWords(name string) []string {
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '_' || r == '-' })
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
