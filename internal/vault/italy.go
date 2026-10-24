package vault

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrForbiddenField is returned for fields that hold authentication secrets.
// SPID credentials and CIE PIN/PUK are strictly personal (AgID) and must stay
// with the user's identity provider or CieID app, never in the vault.
var ErrForbiddenField = errors.New("authentication secrets (SPID credentials, CIE PIN/PUK) must never be stored in the vault")

// ValidationError reports a field value that failed format validation.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid value for %s: %s", e.Field, e.Msg)
}

// IsForbiddenField reports whether a field ID would store an authentication secret.
func IsForbiddenField(id string) bool {
	parts := strings.SplitN(id, ".", 2)
	if len(parts) != 2 {
		return false
	}
	category, name := strings.ToLower(parts[0]), strings.ToLower(parts[1])
	switch category {
	case "spid":
		return true
	case "cie":
		for _, secret := range []string{"pin", "puk"} {
			if name == secret || strings.HasPrefix(name, secret+"_") || strings.HasSuffix(name, "_"+secret) {
				return true
			}
		}
	}
	return false
}

var (
	reCIENumber  = regexp.MustCompile(`^[A-Z]{2}[0-9]{5}[A-Z]{2}$`)
	reCAP        = regexp.MustCompile(`^[0-9]{5}$`)
	reProvincia  = regexp.MustCompile(`^[A-Z]{2}$`)
	rePartitaIVA = regexp.MustCompile(`^[0-9]{11}$`)
)

// NormalizeAndValidate applies Italian format rules to a field value based on
// its ID. It returns the normalized value (e.g. upper-cased codice fiscale) or
// a *ValidationError. Fields without a rule are returned unchanged.
func NormalizeAndValidate(id, value string) (string, error) {
	parts := strings.SplitN(id, ".", 2)
	if len(parts) != 2 {
		return value, nil
	}
	category, name := parts[0], parts[1]

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
	case (category == "residenza" || category == "domicilio") && name == "provincia",
		category == "identity" && name == "birth_province":
		v := normalizeCode(value)
		if !reProvincia.MatchString(v) {
			return "", &ValidationError{Field: id, Msg: "provincia must be a 2-letter sigla (e.g. NA)"}
		}
		return v, nil
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

func isUpperLetter(c byte) bool { return c >= 'A' && c <= 'Z' }
