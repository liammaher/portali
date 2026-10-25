package vault

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Housing categories (abitazione, locazione, utenze, tari) describe the home
// you live in now; its address stays in residenza/domicilio. Codice fiscale,
// IBAN and date fields in them use the generic rules in NormalizeAndValidate.

var (
	reCatastoCategoria = regexp.MustCompile(`^([A-F])/?0*([0-9]{1,2})$`)
	reFoglio           = regexp.MustCompile(`^[0-9]{1,4}$`)
	reParticella       = regexp.MustCompile(`^[0-9A-Z]{1,7}$`)
	reSubalterno       = regexp.MustCompile(`^[0-9]{1,4}$`)
	rePOD              = regexp.MustCompile(`^IT[0-9]{3}E[0-9A-Z]{8,9}$`)
	rePDR              = regexp.MustCompile(`^[0-9]{14}$`)
	reRLICode          = regexp.MustCompile(`^[A-Z0-9]{10,20}$`)
	reDecimal          = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
)

// catastoGroupMax is the highest category number in each cadastral group
// (A/1-A/11, B/1-B/8, C/1-C/7, D/1-D/10, E/1-E/9, F/1-F/7).
var catastoGroupMax = map[byte]int{'A': 11, 'B': 8, 'C': 7, 'D': 10, 'E': 9, 'F': 7}

var energyClasses = map[string]bool{
	"A4": true, "A3": true, "A2": true, "A1": true,
	"B": true, "C": true, "D": true, "E": true, "F": true, "G": true,
}

// titoli maps accepted ways of saying how you hold the home to the stored word.
var titoli = map[string]string{
	"proprietario": "proprietario", "proprietaria": "proprietario", "owner": "proprietario",
	"locatario": "locatario", "conduttore": "locatario", "inquilino": "locatario", "affittuario": "locatario", "tenant": "locatario",
	"comodatario": "comodatario", "usufruttuario": "usufruttuario", "ospite": "ospite", "altro": "altro",
}

// euroFields hold amounts in euro, stored as 1234.56.
var euroFields = map[string]bool{
	"abitazione.catasto_rendita":           true,
	"locazione.canone_mensile":             true,
	"locazione.spese_condominiali_mensili": true,
	"locazione.deposito_cauzionale":        true,
}

// IsHousingCategory reports the categories that describe your home.
func IsHousingCategory(category string) bool {
	switch strings.ToLower(category) {
	case "abitazione", "locazione", "utenze", "tari":
		return true
	}
	return false
}

// normalizeHousing applies the housing rules. handled is false when the field
// has no housing rule, so the generic rules still apply.
func normalizeHousing(id, category, name, value string) (out string, handled bool, err error) {
	if !IsHousingCategory(category) {
		return value, false, nil
	}
	fail := func(msg string) (string, bool, error) {
		return "", true, &ValidationError{Field: id, Msg: msg}
	}
	key := category + "." + name

	switch {
	case key == "abitazione.titolo":
		if t, ok := titoli[strings.ToLower(strings.TrimSpace(value))]; ok {
			return t, true, nil
		}
		return fail("must be proprietario, locatario, comodatario, usufruttuario, ospite or altro")
	case key == "abitazione.catasto_categoria":
		v := normalizeCode(value)
		m := reCatastoCategoria.FindStringSubmatch(v)
		if m == nil {
			return fail("categoria catastale must look like A/2 (a letter A-F and a number)")
		}
		n, _ := strconv.Atoi(m[2])
		if n < 1 || n > catastoGroupMax[m[1][0]] {
			return fail(fmt.Sprintf("group %s has categories %s/1 to %s/%d", m[1], m[1], m[1], catastoGroupMax[m[1][0]]))
		}
		return fmt.Sprintf("%s/%d", m[1], n), true, nil
	case key == "abitazione.catasto_foglio":
		return matchCode(value, reFoglio, fail, "foglio is a number of up to 4 digits")
	case key == "abitazione.catasto_particella":
		return matchCode(value, reParticella, fail, "particella is up to 7 letters or digits")
	case key == "abitazione.catasto_subalterno":
		return matchCode(value, reSubalterno, fail, "subalterno is a number of up to 4 digits")
	case key == "abitazione.classe_energetica":
		v := strings.TrimPrefix(normalizeCode(value), "CLASSE")
		if !energyClasses[v] {
			return fail("classe energetica must be A4, A3, A2, A1, B, C, D, E, F or G")
		}
		return v, true, nil
	case key == "abitazione.vani", hasFieldSuffix(name, "mq"):
		v, ok := normalizeDecimal(value)
		if !ok {
			return fail("must be a positive number (e.g. 72 or 72.5)")
		}
		return v, true, nil
	case euroFields[key]:
		v, ok := normalizeEuro(value)
		if !ok {
			return fail("must be an amount in euro (e.g. 750, 750,00 or 1.250,50)")
		}
		return v, true, nil
	case key == "locazione.preavviso_disdetta_mesi":
		return wholeNumber(value, 0, 24, fail)
	case key == "tari.componenti_tassati":
		return wholeNumber(value, 1, 30, fail)
	case key == "locazione.cedolare_secca":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "si", "sì", "s", "yes", "y", "true":
			return "si", true, nil
		case "no", "n", "false":
			return "no", true, nil
		}
		return fail("must be si or no")
	case key == "locazione.registrazione_codice_identificativo":
		return matchCode(value, reRLICode, fail, "codice identificativo is 10-20 letters and digits, as on the RLI receipt")
	case key == "utenze.luce_pod":
		return matchCode(value, rePOD, fail, "POD must look like IT001E12345678 (IT, 3 digits, E, then 8-9 characters)")
	case key == "utenze.gas_pdr":
		return matchCode(value, rePDR, fail, "PDR must be 14 digits")
	}
	return value, false, nil
}

// matchCode upper-cases and strips spaces, dots and dashes, then checks re.
func matchCode(value string, re *regexp.Regexp, fail func(string) (string, bool, error), msg string) (string, bool, error) {
	v := normalizeCode(value)
	if !re.MatchString(v) {
		return fail(msg)
	}
	return v, true, nil
}

func wholeNumber(value string, lo, hi int, fail func(string) (string, bool, error)) (string, bool, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < lo || n > hi {
		return fail(fmt.Sprintf("must be a whole number from %d to %d", lo, hi))
	}
	return strconv.Itoa(n), true, nil
}

// normalizeDecimal accepts 72, 72.5, 72,5 or "72 mq" and returns the plain number.
func normalizeDecimal(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, unit := range []string{"mq", "m²", "m2", "vani"} {
		s = strings.TrimSpace(strings.TrimSuffix(s, unit))
	}
	s = strings.ReplaceAll(s, ",", ".")
	if !reDecimal.MatchString(s) {
		return "", false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return "", false
	}
	return strconv.FormatFloat(f, 'f', -1, 64), true
}

// normalizeEuro accepts Italian (1.250,50) and plain (1250.50) amounts, with or
// without a € sign, and returns two decimals. A lone dot followed by exactly
// three digits is read as a thousands separator, as in Italian (1.250 = 1250).
func normalizeEuro(s string) (string, bool) {
	s = strings.ToLower(s)
	for _, cut := range []string{"€", "euro", "eur", " "} {
		s = strings.ReplaceAll(s, cut, "")
	}
	lastDot, lastComma := strings.LastIndex(s, "."), strings.LastIndex(s, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastComma > lastDot {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
	case lastComma >= 0:
		if strings.Count(s, ",") > 1 {
			s = strings.ReplaceAll(s, ",", "")
		} else {
			s = strings.Replace(s, ",", ".", 1)
		}
	case lastDot >= 0:
		if strings.Count(s, ".") > 1 || len(s)-lastDot-1 == 3 {
			s = strings.ReplaceAll(s, ".", "")
		}
	}
	if !reDecimal.MatchString(s) {
		return "", false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", false
	}
	return strconv.FormatFloat(f, 'f', 2, 64), true
}
