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

// formeGiuridiche are the accepted legal forms, in the order the schema lists
// them. lavoratore_autonomo is a libero professionista or other self-employed
// person with a partita IVA but no impresa.
var formeGiuridiche = []string{
	"ditta_individuale", "lavoratore_autonomo", "srl", "srls", "snc", "sas", "spa", "sapa",
	"cooperativa", "studio_associato", "stp", "altro",
}

// formaGiuridicaAliases maps ways of writing a legal form, compared with
// lettersAndDigits, to its key. Every key also names itself.
var formaGiuridicaAliases = map[string]string{
	"ditta": "ditta_individuale", "impresaindividuale": "ditta_individuale",
	"liberoprofessionista": "lavoratore_autonomo", "professionista": "lavoratore_autonomo", "autonomo": "lavoratore_autonomo",
	"societaaresponsabilitalimitata": "srl", "srlsemplificata": "srls", "srlunipersonale": "srl",
	"societainnomecollettivo": "snc", "societainaccomanditasemplice": "sas", "societaperazioni": "spa",
	"coop": "cooperativa", "societacooperativa": "cooperativa", "scarl": "cooperativa",
	"associazioneprofessionale": "studio_associato", "societatraprofessionisti": "stp",
	"other": "altro",
}

func init() {
	for _, f := range formeGiuridiche {
		formaGiuridicaAliases[lettersAndDigits(f)] = f
	}
	// Accents fold away in lettersAndDigits-compared words like società.
	for k, v := range formaGiuridicaAliases {
		formaGiuridicaAliases[strings.ReplaceAll(k, "societa", "società")] = v
	}
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
	case key == "business.forma_giuridica":
		if f, ok := formaGiuridicaAliases[lettersAndDigits(value)]; ok {
			return f, true, nil
		}
		return fail("must be one of " + strings.Join(formeGiuridiche, ", "))
	case key == "business.cciaa":
		v := strings.ToUpper(strings.TrimSpace(value))
		if !reProvincia.MatchString(v) {
			return fail("must be the provincia's 2-letter sigla (e.g. NA)")
		}
		return v, true, nil
	case key == "business.capitale_sociale":
		v, ok := normalizeEuro(value)
		if !ok {
			return fail("must be an amount in euro (e.g. 10.000,00 or 10000)")
		}
		return v, true, nil
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
