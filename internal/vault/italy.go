package vault

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TierPersonal is the human-only sensitivity tier. Personal fields and
// documents are never readable, listable or writable by any service token,
// whatever its scope or cap; only the user's own session (CLI or onboarding
// page, both gated on the password) can see them.
const TierPersonal = "personal"

// ValidationError reports a field value that failed format validation.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid value for %s: %s", e.Field, e.Msg)
}

// reCredentialWord matches a field-name word that names a login secret
// (pin, pin1, puk2, password, otp, username, ...).
var reCredentialWord = regexp.MustCompile(`^(pin[0-9]*|puk[0-9]*|password|passwd|pwd|pw|pass|passcode|otp|totp|username|login|secret|segreto|chiave)$`)

// IsCredentialField reports whether a field holds a login secret: a
// credential word (pin, puk, password, otp, username, ...) in a field about
// SPID, CieID or the CIE, e.g. spid.password, cie.pin1, identity.spid_password.
// Credentials are strictly personal (AgID), so they can only be stored with
// TierPersonal and never reach an agent.
func IsCredentialField(id string) bool {
	parts := strings.SplitN(strings.ToLower(id), ".", 2)
	if len(parts) != 2 {
		return false
	}
	category, name := parts[0], parts[1]
	// In SPID/CieID categories every field is treated as a credential except
	// known non-secret details, so a secret stored under an unexpected name
	// can't be lowered to an agent-visible tier.
	if IsPersonalCategory(category) {
		return !isNonSecretAccountField(name)
	}

	words := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	aboutIdentity := category == "cie"
	for _, w := range words {
		if w == "spid" || w == "cie" || w == "cieid" {
			aboutIdentity = true
		}
	}
	if !aboutIdentity {
		return false
	}
	for _, w := range words {
		if reCredentialWord.MatchString(w) {
			return true
		}
	}
	return false
}

// isNonSecretAccountField lists SPID/CieID fields that may be lowered from
// personal: the provider, the level, and dates such as the renewal date.
func isNonSecretAccountField(name string) bool {
	switch strings.ToLower(name) {
	case "provider", "livello", "level":
		return true
	}
	return IsDateField(name)
}

// IsPersonalCategory reports categories whose fields and documents default to
// TierPersonal (SPID and CieID accounts).
func IsPersonalCategory(category string) bool {
	c := strings.ToLower(category)
	return c == "spid" || c == "cieid"
}

// errCredentialTier is returned when a credential would be stored below
// TierPersonal.
func errCredentialTier(field string) error {
	return &ValidationError{Field: field, Msg: "SPID/CIE credentials can only be stored as personal (never readable by agents)"}
}

var (
	reCIENumber  = regexp.MustCompile(`^[A-Z]{2}[0-9]{5}[A-Z]{2}$`)
	reCAP        = regexp.MustCompile(`^[0-9]{5}$`)
	reProvincia  = regexp.MustCompile(`^[A-Z]{2}$`)
	rePartitaIVA = regexp.MustCompile(`^[0-9]{11}$`)
	reIBAN       = regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}$`)
	reCIECode    = regexp.MustCompile(`^[0-9]{8}$`)
	// reInternoPrefix strips a leading "interno", "int." or "int" from a flat number.
	reInternoPrefix = regexp.MustCompile(`^(?i)\s*(interno|int\.?)\s*`)
)

// NormalizeAndValidate applies Italian format rules to a field value based on
// its ID (matched case-insensitively). It returns the normalized value (e.g.
// upper-cased codice fiscale) or a *ValidationError. Fields without a rule
// are returned unchanged.
func NormalizeAndValidate(id, value string) (string, error) {
	parts := strings.SplitN(strings.ToLower(id), ".", 2)
	if len(parts) != 2 {
		return value, nil
	}
	category, name := parts[0], parts[1]

	if v, handled, err := normalizeHousing(id, category, name, value); handled {
		return v, err
	}

	switch {
	case hasFieldSuffix(name, "codice_fiscale"):
		cf := normalizeCode(value)
		if err := ValidateCodiceFiscale(cf); err != nil {
			return "", &ValidationError{Field: id, Msg: err.Error()}
		}
		return cf, nil
	case hasFieldSuffix(name, "partita_iva"):
		piva := strings.TrimPrefix(normalizeCode(value), "IT")
		if err := ValidatePartitaIVA(piva); err != nil {
			return "", &ValidationError{Field: id, Msg: err.Error()}
		}
		return piva, nil
	case hasFieldSuffix(name, "iban"):
		iban := normalizeCode(value)
		if err := ValidateIBAN(iban); err != nil {
			return "", &ValidationError{Field: id, Msg: err.Error()}
		}
		return iban, nil
	case category == "cie" && (name == "pin" || name == "puk"):
		v := strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' {
				return -1
			}
			return r
		}, value)
		if !reCIECode.MatchString(v) {
			return "", &ValidationError{Field: id, Msg: "CIE PIN and PUK are 8 digits (both halves together)"}
		}
		return v, nil
	case category == "cie" && name == "numero":
		n := normalizeCode(value)
		if !reCIENumber.MatchString(n) {
			return "", &ValidationError{Field: id, Msg: "CIE number must look like CA12345AB (2 letters, 5 digits, 2 letters)"}
		}
		return n, nil
	case (category == "residenza" || category == "domicilio") && name == "cap":
		v := strings.TrimSpace(value)
		if !reCAP.MatchString(v) {
			return "", &ValidationError{Field: id, Msg: "CAP must be 5 digits"}
		}
		return v, nil
	case (category == "residenza" || category == "domicilio") && name == "interno":
		v := strings.TrimSpace(reInternoPrefix.ReplaceAllString(value, ""))
		if v == "" || len(v) > 10 {
			return "", &ValidationError{Field: id, Msg: "interno is the flat number, e.g. 13 or 4B"}
		}
		return v, nil
	case (category == "residenza" || category == "domicilio") && name == "provincia",
		category == "identity" && name == "birth_province":
		v := normalizeCode(value)
		if !reProvincia.MatchString(v) {
			return "", &ValidationError{Field: id, Msg: "provincia must be a 2-letter sigla (e.g. NA)"}
		}
		return v, nil
	case IsDateField(name):
		d, err := NormalizeDate(value)
		if err != nil {
			return "", &ValidationError{Field: id, Msg: err.Error()}
		}
		return d, nil
	}
	return value, nil
}

// hasFieldSuffix matches "suffix" exactly or as a trailing "_suffix",
// so nucleo_familiare.membro1_codice_fiscale is validated like identity.codice_fiscale.
func hasFieldSuffix(name, suffix string) bool {
	return name == suffix || strings.HasSuffix(name, "_"+suffix)
}

// normalizeCode upper-cases and strips spaces, dots and dashes.
func normalizeCode(s string) string {
	s = strings.ToUpper(s)
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '.', '-':
			return -1
		}
		return r
	}, s)
}

// Codice fiscale check-character tables (DM 23/12/1976).
var cfOddValues = map[byte]int{
	'0': 1, '1': 0, '2': 5, '3': 7, '4': 9, '5': 13, '6': 15, '7': 17, '8': 19, '9': 21,
	'A': 1, 'B': 0, 'C': 5, 'D': 7, 'E': 9, 'F': 13, 'G': 15, 'H': 17, 'I': 19, 'J': 21,
	'K': 2, 'L': 4, 'M': 18, 'N': 20, 'O': 11, 'P': 3, 'Q': 6, 'R': 8, 'S': 12, 'T': 14,
	'U': 16, 'V': 10, 'W': 22, 'X': 25, 'Y': 24, 'Z': 23,
}

const (
	cfMonthLetters = "ABCDEHLMPRST"
	// cfOmocodia maps digits 0-9 to the letters used when resolving omocodia.
	cfOmocodia = "LMNPQRSTUV"
)

// cfDigitPositions are the indexes that hold digits (or omocodia letters).
var cfDigitPositions = []int{6, 7, 9, 10, 12, 13, 14}

// ValidateCodiceFiscale checks a normalized (upper-case, no spaces) codice fiscale.
// It accepts the 16-character personal code, including omocodia variants, and
// the 11-digit numeric code issued to legal entities and as a temporary code.
func ValidateCodiceFiscale(cf string) error {
	if len(cf) == 11 {
		if err := ValidatePartitaIVA(cf); err != nil {
			return errors.New("11-digit codice fiscale has an invalid check digit")
		}
		return nil
	}
	if len(cf) != 16 {
		return fmt.Errorf("codice fiscale must be 16 characters (or 11 digits), got %d", len(cf))
	}

	// Resolve omocodia so date/place checks see real digits.
	resolved := []byte(cf)
	for _, i := range cfDigitPositions {
		c := resolved[i]
		if c >= '0' && c <= '9' {
			continue
		}
		idx := strings.IndexByte(cfOmocodia, c)
		if idx < 0 {
			return fmt.Errorf("unexpected character %q at position %d", c, i+1)
		}
		resolved[i] = byte('0' + idx)
	}

	for i := 0; i < 6; i++ {
		if !isUpperLetter(cf[i]) {
			return errors.New("first 6 characters (surname and name) must be letters")
		}
	}
	if strings.IndexByte(cfMonthLetters, cf[8]) < 0 {
		return fmt.Errorf("invalid month letter %q", cf[8])
	}
	day := int(resolved[9]-'0')*10 + int(resolved[10]-'0')
	if day > 40 {
		day -= 40 // women have 40 added to the day of birth
	}
	if day < 1 || day > 31 {
		return errors.New("invalid day of birth")
	}
	if !isUpperLetter(cf[11]) {
		return errors.New("birthplace code must start with a letter")
	}
	if !isUpperLetter(cf[15]) {
		return errors.New("check character must be a letter")
	}

	if want := codiceFiscaleCheckChar(cf[:15]); cf[15] != want {
		return errors.New("check character does not match")
	}
	return nil
}

// codiceFiscaleCheckChar computes the 16th character from the first 15.
// Positions are 1-indexed in the spec: odd positions use cfOddValues, even
// positions use the plain value (digits 0-9, letters A=0..Z=25).
func codiceFiscaleCheckChar(first15 string) byte {
	sum := 0
	for i := 0; i < 15; i++ {
		c := first15[i]
		if i%2 == 0 {
			sum += cfOddValues[c]
		} else if c >= '0' && c <= '9' {
			sum += int(c - '0')
		} else {
			sum += int(c - 'A')
		}
	}
	return byte('A' + sum%26)
}

// ValidatePartitaIVA checks an 11-digit partita IVA using its Luhn-style check digit.
func ValidatePartitaIVA(piva string) error {
	if !rePartitaIVA.MatchString(piva) {
		return errors.New("partita IVA must be 11 digits")
	}
	sum := 0
	for i := 0; i < 10; i++ {
		d := int(piva[i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	if check := (10 - sum%10) % 10; check != int(piva[10]-'0') {
		return errors.New("partita IVA check digit does not match")
	}
	return nil
}

// ValidateIBAN checks a normalized IBAN's format, Italian length (27) and
// ISO 13616 mod-97 check digits.
func ValidateIBAN(iban string) error {
	if !reIBAN.MatchString(iban) {
		return errors.New("IBAN must be a country code, 2 check digits and the account number (e.g. IT60X0542811101000000123456)")
	}
	if strings.HasPrefix(iban, "IT") && len(iban) != 27 {
		return fmt.Errorf("an Italian IBAN has 27 characters, got %d", len(iban))
	}
	// Move the first four characters to the end, map letters to 10..35 and
	// compute the remainder mod 97 digit by digit.
	rearranged := iban[4:] + iban[:4]
	rem := 0
	for i := 0; i < len(rearranged); i++ {
		c := rearranged[i]
		if c >= 'A' && c <= 'Z' {
			v := int(c-'A') + 10
			rem = (rem*100 + v) % 97
		} else {
			rem = (rem*10 + int(c-'0')) % 97
		}
	}
	if rem != 1 {
		return errors.New("IBAN check digits do not match — check for a typo")
	}
	return nil
}

func isUpperLetter(c byte) bool { return c >= 'A' && c <= 'Z' }
