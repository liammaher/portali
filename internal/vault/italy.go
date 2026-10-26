package vault

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
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

const (
	// secretWords name a secret in any category (fisconline.pin), a card's
	// security code (payment.cvv, cvc2) included.
	secretWords = `(?:pin|puk|pincode|pukcode|password|passwd|pwd|pw|passcode|otp|totp|secret|segreto|segreta|segreti|segrete|credenziale|credenziali|sblocco|cvv|cvc|cvn)s?[0-9]*`
	// credentialWords add words that name a login detail only next to SPID,
	// CieID or the CIE (spid.username, spid.pass): elsewhere they are
	// everyday words (pec.username, locazione.chiavi_consegnate, pass_ztl).
	credentialWords = `(?:` + secretWords + `|(?:pass|username|login|chiave|chiavi)s?[0-9]*)`
	// A trailing number names a second account or card: spid2, cie2. firma,
	// firmadigitale, cns and smartcard cover a digital signature or CNS
	// (firma_digitale.pin, cns.puk).
	identityWords = `(?:spid|cieid|cie|firma|firmadigitale|cns|smartcard)[0-9]*`
)

var (
	// reCredentialWord matches a field-name word that names a login secret
	// (pin, pin1, puk2, password, passwords, password2, otp, username, ...).
	reCredentialWord = regexp.MustCompile(`^(` + credentialWords + `)$`)
	// reSecretWord matches the credential words that are a secret anywhere.
	reSecretWord = regexp.MustCompile(`^(` + secretWords + `)$`)
	// reIdentityWord matches a word naming SPID, CieID or the CIE.
	reIdentityWord = regexp.MustCompile(`^(` + identityWords + `)$`)
	// rePersonalCategoryWord matches the first word of a SPID or CieID
	// account category (not the CIE itself, whose number isn't secret).
	rePersonalCategoryWord = regexp.MustCompile(`^(spid|cieid)[0-9]*$`)
	// reJoinedCredential matches an identity word and a credential word run
	// together, as in identity.spidPassword (lower-cased to spidpassword).
	reJoinedCredential = regexp.MustCompile(`^(` + identityWords + `)(` + credentialWords + `)$|^(` + credentialWords + `)(` + identityWords + `)$`)
)

// IsCredentialField reports whether a field holds a login secret: any field
// whose category or name has a secret word (pin, puk, password, otp, ...),
// e.g. fisconline.pin, entratel.password, cie.pin1; one with a wider
// credential word (username, login, pass, chiave) next to SPID, CieID or the
// CIE, e.g. spid.username, identity.spid_login; and every field of a
// SPID/CieID account.
// A person's field (leo:spid.password) is judged like the owner's.
// Credentials can only be stored with TierPersonal, so they never reach an
// agent without the user's approval.
func IsCredentialField(id string) bool {
	return isCredential(id, false)
}

// IsIdentityCredential reports a credential tied to a person's identity: a
// SPID/CieID account, or a credential word in a field about SPID, CieID, the
// CIE or a digital signature (cie.pin, firma_digitale.puk,
// identity.spid_password). Unlike a portal login such as Entratel's, only a
// SPID account can be a business's own (checkKind).
func IsIdentityCredential(id string) bool {
	return isCredential(id, true)
}

func isCredential(id string, identityOnly bool) bool {
	_, id = SplitPerson(id)
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
	// A pratica's campi are fixed and none is a secret, so a slug like
	// cie-pin-ristampa doesn't make the pratica personal.
	if _, _, ok := praticaField(category, name); ok {
		return false
	}

	aboutIdentity, credentialWord, secretWord := false, false, false
	for _, w := range idWords(category + "_" + name) {
		switch {
		case reJoinedCredential.MatchString(w):
			return true
		case reIdentityWord.MatchString(w):
			aboutIdentity = true
		case reCredentialWord.MatchString(w):
			credentialWord = true
			secretWord = secretWord || reSecretWord.MatchString(w)
		}
	}
	return credentialWord && aboutIdentity || secretWord && !identityOnly
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
// TierPersonal: SPID and CieID accounts, including ones named per provider
// such as spid_poste, cieid-2 or spid2, for the owner or another person (leo:spid).
func IsPersonalCategory(category string) bool {
	_, category = SplitPerson(category)
	words := idWords(strings.ToLower(category))
	return len(words) > 0 && rePersonalCategoryWord.MatchString(words[0])
}

// idWords splits a category or field name into its words.
func idWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
}

// errCredentialTier is returned when a credential would be stored below
// TierPersonal.
func errCredentialTier(field string) error {
	return &ValidationError{Field: field, Msg: "login secrets (PIN, password, ...) can only be stored as personal (never readable by agents)"}
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

// oneOnly are the categories a holder has one of, so no numbered copies:
// the rules that read them (Household, retention, Expiring) read these names.
var oneOnly = []string{"famiglia", "pratiche", "ruolo"}

// NormalizeAndValidate applies Italian format rules to a field value based on
// its ID (matched case-insensitively). It returns the normalized value (e.g.
// upper-cased codice fiscale) or a *ValidationError. Fields without a rule
// are returned unchanged. A person's field (leo:cie.numero) gets the same
// rules as the owner's; errors name the full ID.
func NormalizeAndValidate(id, value string) (string, error) {
	person, local := SplitPerson(id)
	parts := strings.SplitN(canonicalLocalID(local), ".", 2) // cie2.numero is a cie.numero
	if len(parts) != 2 {
		return value, nil
	}
	category, name := parts[0], parts[1]
	if written, _, _ := strings.Cut(strings.ToLower(local), "."); written != category && slices.Contains(oneOnly, category) {
		return "", &ValidationError{Field: id, Msg: category + " has no numbered copies: it is one record per person (pratiche: one per slug), so use " + category + ".*"}
	}

	if err := checkHolder(id); err != nil {
		return "", err
	}
	if v, handled, err := normalizeFamiglia(id, person, category, name, value); handled {
		return v, err
	}
	if v, handled, err := normalizeRuolo(id, category, name, value); handled {
		return v, err
	}

	if v, handled, err := normalizeHousing(id, category, name, value); handled {
		return v, err
	}
	if v, handled, err := normalizeBusiness(id, category, name, value); handled {
		return v, err
	}
	if v, handled, err := normalizePratica(id, category, name, value); handled {
		return v, err
	}
	if v, handled, err := normalizeAccess(id, category, name, value); handled {
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
	case addressPart(category, name, "cap"):
		v := strings.TrimSpace(value)
		if !reCAP.MatchString(v) {
			return "", &ValidationError{Field: id, Msg: "CAP must be 5 digits"}
		}
		return v, nil
	case addressPart(category, name, "interno"):
		v := strings.TrimSpace(reInternoPrefix.ReplaceAllString(value, ""))
		if v == "" || len(v) > 10 {
			return "", &ValidationError{Field: id, Msg: "interno is the flat number, e.g. 13 or 4B"}
		}
		return v, nil
	case addressPart(category, name, "provincia"),
		category == "identity" && name == "birth_province":
		v := normalizeCode(value)
		if !reProvincia.MatchString(v) {
			return "", &ValidationError{Field: id, Msg: "provincia must be a 2-letter sigla (e.g. NA)"}
		}
		return v, nil
	case category == "identity" && name == "birth_country":
		v := strings.TrimSpace(value)
		switch strings.ToLower(v) {
		case "italia", "italy", "it", "ita", "repubblica italiana":
			return "", &ValidationError{Field: id, Msg: "birth_country is only for births abroad; for Italy set identity.birth_comune and birth_province"}
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
// so locazione.locatore_codice_fiscale is validated like identity.codice_fiscale.
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
	month := strings.IndexByte(cfMonthLetters, cf[8])
	if month < 0 {
		return fmt.Errorf("invalid month letter %q", cf[8])
	}
	day := int(resolved[9]-'0')*10 + int(resolved[10]-'0')
	if day > 40 {
		day -= 40 // women have 40 added to the day of birth
	}
	// The century isn't encoded, but 19yy and 20yy are leap years alike,
	// except 1900: 29 February is possible when yy is a multiple of 4.
	year := int(resolved[6]-'0')*10 + int(resolved[7]-'0')
	maxDay := [12]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[month]
	if month == 1 && year%4 == 0 {
		maxDay = 29
	}
	if day < 1 || day > maxDay {
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
	// Digits 8-10 are the issuing office, never 000 (so 00000000000 fails).
	if piva[7:10] == "000" {
		return errors.New("partita IVA office code (digits 8-10) can't be 000")
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
