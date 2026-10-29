package vault

import "strings"

// immobile is the property a pratica is about (building works, a client's
// flat): its address and catasto. Anyone may hold one, the owner or a person
// (luca:immobile), in either kind of vault: a property is neither a person
// nor a business, so the category has no subject. It is not housing: the home
// stays at residenza/domicilio and abitazione. A second property is
// immobile2. The address parts (CAP, provincia, interno) use the generic
// rules in NormalizeAndValidate, the computed quartiere, municipalità and
// distretto located.go.

// immobileTitoli are the holder's titles on the property, in the order the
// schema lists them (schemaValues).
var immobileTitoli = []string{"proprietario", "comproprietario", "locatario", "usufruttuario", "altro"}

// immobileTitoloAliases maps ways of saying a title, read with normalizeChoice,
// to the stored word. Every title also names itself.
var immobileTitoloAliases = map[string]string{
	"proprietaria": "proprietario", "owner": "proprietario",
	"comproprietaria": "comproprietario", "co_proprietario": "comproprietario", "co_proprietaria": "comproprietario",
	"co_owner": "comproprietario", "coowner": "comproprietario",
	"locataria": "locatario", "conduttore": "locatario", "affittuario": "locatario", "affittuaria": "locatario",
	"inquilino": "locatario", "inquilina": "locatario", "tenant": "locatario",
	"usufruttuaria": "usufruttuario",
	"other":         "altro",
}

func init() {
	for _, t := range immobileTitoli {
		immobileTitoloAliases[t] = t
	}
}

// normalizeImmobile applies the immobile rules. handled is false when the
// field has no rule here, so the generic rules still apply.
func normalizeImmobile(id, category, name, value string) (out string, handled bool, err error) {
	if category != "immobile" {
		return value, false, nil
	}
	fail := func(msg string) (string, bool, error) {
		return "", true, &ValidationError{Field: id, Msg: msg}
	}
	if v, handled, err := normalizeCatasto(name, value, fail); handled {
		return v, handled, err
	}
	switch {
	case name == "titolo":
		if t, ok := immobileTitoloAliases[normalizeChoice(value)]; ok {
			return t, true, nil
		}
		return fail("must be one of " + strings.Join(immobileTitoli, ", "))
	case hasFieldSuffix(name, "mq"):
		v, ok := normalizeDecimal(value)
		if !ok {
			return fail("must be a positive number (e.g. 85 or 85.5)")
		}
		return v, true, nil
	}
	return value, false, nil
}
