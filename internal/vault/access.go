package vault

import (
	"strings"
	"unicode"
)

// How the user logs in to government portals: preferences.accesso_pa picks
// SPID or CIE, and spid.provider names the SPID identity provider. Both are
// stored in a fixed spelling so an agent can go straight to the right button.

// accessiPA maps accepted ways of naming a login method to the stored word.
var accessiPA = map[string]string{
	"spid": "spid", "cie": "cie", "cieid": "cie",
}

// spidProviders maps a provider name, lower-cased with everything but letters
// and digits removed, to the spelling used by the onboarding page. The list is
// the providers on ANPR's SPID login page (checked 2026-10-25).
var spidProviders = map[string]string{
	"posteid": "PosteID", "poste": "PosteID", "posteitaliane": "PosteID",
	"aruba": "Aruba", "arubaid": "Aruba",
	"infocert": "InfoCert", "infocertid": "InfoCert",
	"namirial": "Namirial", "namirialid": "Namirial",
	"sielte": "Sielte", "sielteid": "Sielte",
	"lepida": "Lepida", "lepidaid": "Lepida",
	"register": "Register", "registerit": "Register", "spiditalia": "Register", "spiditaliaregisterit": "Register",
	"timid": "TIM id", "tim": "TIM id", "titt": "TIM id", "tittid": "TIM id", "titrusttechnologies": "TIM id",
	"intesigroup": "Intesi Group", "intesi": "Intesi Group", "intesiid": "Intesi Group", "intesigroupid": "Intesi Group",
	"etnaid": "EtnaID", "etna": "EtnaID", "etnahitech": "EtnaID",
	"teamsystem": "TeamSystem", "teamsystemid": "TeamSystem",
	"infocamere": "InfoCamere", "infocamereid": "InfoCamere",
}

// spidProviderNames are the stored spellings, in the order the error message
// and the schema list them.
var spidProviderNames = []string{"PosteID", "Aruba", "InfoCert", "Namirial", "Sielte", "Lepida",
	"Register", "TIM id", "Intesi Group", "EtnaID", "TeamSystem", "InfoCamere"}

var spidProviderList = orList(spidProviderNames)

// orList joins items as "a, b or c".
func orList(items []string) string {
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}

// normalizeAccess applies the login-method rules. handled is false for every
// other field, so the generic rules still apply.
func normalizeAccess(id, category, name, value string) (out string, handled bool, err error) {
	var table map[string]string
	var msg string
	switch category + "." + name {
	case "preferences.accesso_pa":
		table, msg = accessiPA, "must be spid or cie"
	case "spid.provider":
		table, msg = spidProviders, "must be one of "+spidProviderList
	default:
		return value, false, nil
	}
	if v, ok := table[lettersAndDigits(value)]; ok {
		return v, true, nil
	}
	return "", true, &ValidationError{Field: id, Msg: msg}
}

// lettersAndDigits lower-cases s and drops everything but letters and digits,
// so "Poste ID", "poste-id" and "PosteID" compare equal.
func lettersAndDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
