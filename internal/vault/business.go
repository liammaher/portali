package vault

import "strings"

// The business category describes your impresa (partita IVA, sede legale);
// locale is the premises where the activity is carried on, one unit, like
// abitazione for the home but with its own address. The address parts (CAP,
// provincia, interno) and dates use the generic rules in NormalizeAndValidate.

// localeTitoli maps accepted ways of saying how the business holds its
// premises to the stored word.
var localeTitoli = map[string]string{
	"proprietario": "proprietario", "proprietaria": "proprietario", "owner": "proprietario",
	"locatario": "locatario", "locataria": "locatario", "conduttore": "locatario", "affittuario": "locatario",
	"affittuaria": "locatario", "tenant": "locatario",
	"comodatario": "comodatario", "comodataria": "comodatario", "usufruttuario": "usufruttuario", "usufruttuaria": "usufruttuario",
	"altro": "altro",
}

// qualitaDichiarante maps the ways of naming the signer's role to the stored word.
var qualitaDichiarante = map[string]string{
	"titolare": "titolare", "titolare_ditta_individuale": "titolare",
	"legale_rappresentante": "legale_rappresentante", "rappresentante_legale": "legale_rappresentante",
	"legale_rapp": "legale_rappresentante",
}

// isBusinessCategory reports the categories about the user's impresa.
func isBusinessCategory(category string) bool {
	return category == "business" || category == "locale"
}

// normalizeBusiness applies the business and locale rules. handled is false
// when the field has no rule here, so the generic rules still apply.
func normalizeBusiness(id, category, name, value string) (out string, handled bool, err error) {
	if !isBusinessCategory(category) {
		return value, false, nil
	}
	fail := func(msg string) (string, bool, error) {
		return "", true, &ValidationError{Field: id, Msg: msg}
	}
	key := category + "." + name

	if category == "locale" {
		if v, handled, err := normalizeCatasto(name, value, fail); handled {
			return v, handled, err
		}
	}
	switch {
	case key == "business.qualita_dichiarante":
		if q, ok := qualitaDichiarante[normalizeChoice(value)]; ok {
			return q, true, nil
		}
		return fail("must be titolare or legale_rappresentante")
	case key == "locale.titolo":
		if t, ok := localeTitoli[strings.ToLower(strings.TrimSpace(value))]; ok {
			return t, true, nil
		}
		return fail("must be proprietario, locatario, comodatario, usufruttuario or altro")
	case category == "locale" && hasFieldSuffix(name, "mq"):
		v, ok := normalizeDecimal(value)
		if !ok {
			return fail("must be a positive number (e.g. 45 or 45.5)")
		}
		return v, true, nil
	}
	return value, false, nil
}

// addressPart reports whether name is the given part (cap, provincia,
// interno) of an address: residenza, domicilio and locale name it directly,
// business as the sede legale's (sede_legale_cap).
func addressPart(category, name, part string) bool {
	switch category {
	case "residenza", "domicilio", "locale":
		return name == part
	case "business":
		return name == "sede_legale_"+part
	}
	return false
}
