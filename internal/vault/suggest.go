package vault

import "strings"

// Suggestion is returned when a non-canonical field has a similar canonical name.
type Suggestion struct {
	Canonical   string `json:"canonical"`
	Description string `json:"description"`
	Reason      string `json:"reason"` // "synonym" or "similar"
}

// synonyms maps common alternative field names to their canonical equivalents.
// Keys are field names (without category prefix); values are canonical field names.
var synonyms = map[string]string{
	"name":          "full_name",
	"fullname":      "full_name",
	"display_name":  "full_name",
	"firstname":     "first_name",
	"given_name":    "first_name",
	"lastname":      "last_name",
	"family_name":   "last_name",
	"surname":       "last_name",
	"mail":          "email",
	"email_address": "email",
	"dob":           "date_of_birth",
	"birthday":      "date_of_birth",
	"birth_date":    "date_of_birth",
	"birthdate":     "date_of_birth",
	"telephone":     "phone",
	"phone_number":  "phone",
	"mobile":        "phone",
	// addresses
	"street":       "home_street",
	"address":      "home_street",
	"city":         "home_city",
	"state":        "home_state",
	"province":     "home_state",
	"zip":          "home_zip",
	"zipcode":      "home_zip",
	"zip_code":     "home_zip",
	"postal_code":  "home_zip",
	"postal":       "home_zip",
	"country":      "home_country",
	"country_code": "home_country",

	// financial
	"social_security":        "ssn",
	"social_security_number": "ssn",

	// payment
	"cc_number":   "card_number",
	"card_num":    "card_number",
	"cc_expiry":   "card_expiry",
	"expiry":      "card_expiry",
	"expiry_date": "card_expiry",
	"exp_date":    "card_expiry",
	"card_name":   "cardholder_name",

	// employment
	"company":  "employer",
	"job":      "title",
	"position": "title",
	"role":     "title",

	// preferences
	"tz":     "timezone",
	"lang":   "language",
	"locale": "language",

	// italian identity and documents
	"cf":            "codice_fiscale",
	"tax_code":      "codice_fiscale",
	"fiscal_code":   "codice_fiscale",
	"codicefiscale": "codice_fiscale",
	"gender":        "sex",
	"sesso":         "sex",
	"cittadinanza":  "citizenship",
	"nationality":   "citizenship",
	"number":        "numero",
	"numero_carta":  "numero",
	"scadenza":      "data_scadenza",
	"rilascio":      "data_rilascio",

	// residenza / domicilio
	"via":             "indirizzo",
	"numero_civico":   "civico",
	"int":             "interno",
	"numero_interno":  "interno",
	"flat":            "interno",
	"apartment":       "interno",
	"codice_postale":  "cap",
	"sigla_provincia": "provincia",

	// contacts and bank
	"cellulare":         "phone",
	"telefono":          "phone",
	"posta_certificata": "pec",
	"conto_corrente":    "iban",
	"iban_code":         "iban",

	// housing
	"titolo_godimento":    "titolo",
	"sezione":             "catasto_sezione",
	"foglio":              "catasto_foglio",
	"particella":          "catasto_particella",
	"mappale":             "catasto_particella",
	"subalterno":          "catasto_subalterno",
	"sub":                 "catasto_subalterno",
	"categoria_catastale": "catasto_categoria",
	"categoria":           "catasto_categoria",
	"rendita":             "catasto_rendita",
	"rendita_catastale":   "catasto_rendita",
	"superficie":          "superficie_calpestabile_mq",
	"mq":                  "superficie_calpestabile_mq",
	"ape":                 "classe_energetica",
	"affitto":             "canone_mensile",
	"canone":              "canone_mensile",
	"rent":                "canone_mensile",
	"cauzione":            "deposito_cauzionale",
	"deposito":            "deposito_cauzionale",
	"condominio":          "spese_condominiali_mensili",
	"cedolare":            "cedolare_secca",
	"proprietario":        "locatore_nome",
	"landlord":            "locatore_nome",
	"codice_contratto":    "registrazione_codice_identificativo",
	"pod":                 "luce_pod",
	"pdr":                 "gas_pdr",
	"cod_contr":           "codice_contribuente",

	// business
	"piva":            "partita_iva",
	"p_iva":           "partita_iva",
	"vat":             "partita_iva",
	"vat_number":      "partita_iva",
	"ateco":           "codice_ateco",
	"ragione_sociale": "denominazione",
}

// SuggestCanonical returns a suggestion if the given field ID is not canonical
// but matches a synonym or is similar to a canonical field in the same category.
// Returns nil if the field is already canonical or no match is found.
// A member's field (leo:identity.nome) gets the member's canonical ID.
func SuggestCanonical(id string) *Suggestion {
	member, local := SplitMember(id)
	s := suggestCanonical(local)
	if s != nil && member != "" {
		s.Canonical = member + ":" + s.Canonical
	}
	return s
}

func suggestCanonical(id string) *Suggestion {
	if IsCanonicalField(id) {
		return nil
	}

	parts := strings.SplitN(id, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	category, fieldName := parts[0], parts[1]

	// Tier 1: synonym lookup
	if canonical, ok := synonyms[fieldName]; ok {
		candidateID := category + "." + canonical
		if sf := GetSchemaField(candidateID); sf != nil {
			return &Suggestion{
				Canonical:   candidateID,
				Description: sf.Description,
				Reason:      "synonym",
			}
		}
	}

	// Tier 2: Levenshtein distance within same category
	threshold := len(fieldName) / 3
	if threshold < 2 {
		threshold = 2
	}

	var best *Suggestion
	bestDist := threshold + 1

	for _, cat := range RecommendedSchema.Categories {
		if cat.Name != category {
			continue
		}
		for _, sf := range cat.Fields {
			schemaFieldName := strings.SplitN(sf.ID, ".", 2)[1]
			dist := levenshtein(fieldName, schemaFieldName)
			if dist > 0 && dist <= threshold && dist < bestDist {
				bestDist = dist
				best = &Suggestion{
					Canonical:   sf.ID,
					Description: sf.Description,
					Reason:      "similar",
				}
			}
		}
	}

	return best
}

// levenshtein computes the Levenshtein edit distance between two strings.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Use single-row DP
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			curr[j] = min(ins, min(del, sub))
		}
		prev = curr
	}
	return prev[len(b)]
}
