package vault

import "strings"

// SchemaField describes a recommended field in the vault schema.
type SchemaField struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Sensitivity string `json:"sensitivity"`
}

// SchemaCategory groups recommended fields under a category.
type SchemaCategory struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Fields      []SchemaField `json:"fields"`
}

// Schema is the full recommended schema for the vault.
type Schema struct {
	Version    string           `json:"version"`
	Categories []SchemaCategory `json:"categories"`
}

// RecommendedSchema is the canonical schema that agents can discover.
var RecommendedSchema = Schema{
	Version: "2",
	Categories: []SchemaCategory{
		{
			Name:        "identity",
			Description: "Personal identity information",
			Fields: []SchemaField{
				{ID: "identity.first_name", Description: "First/given name", Sensitivity: "standard"},
				{ID: "identity.last_name", Description: "Last/family name", Sensitivity: "standard"},
				{ID: "identity.full_name", Description: "Full display name", Sensitivity: "standard"},
				{ID: "identity.email", Description: "Primary email address", Sensitivity: "standard"},
				{ID: "identity.phone", Description: "Phone number", Sensitivity: "sensitive"},
				{ID: "identity.date_of_birth", Description: "Date of birth", Sensitivity: "sensitive"},
				{ID: "identity.codice_fiscale", Description: "Italian codice fiscale (16 characters, validated)", Sensitivity: "sensitive"},
				{ID: "identity.sex", Description: "Sex as on Italian documents (M or F)", Sensitivity: "standard"},
				{ID: "identity.birth_comune", Description: "Comune (or foreign country) of birth", Sensitivity: "standard"},
				{ID: "identity.birth_province", Description: "Provincia of birth as 2-letter sigla (e.g. NA), or EE if born abroad", Sensitivity: "standard"},
				{ID: "identity.citizenship", Description: "Citizenship (e.g. italiana, britannica)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "cie",
			Description: "Carta d'identità elettronica details. Never store the PIN or PUK — those are rejected.",
			Fields: []SchemaField{
				{ID: "cie.numero", Description: "CIE number printed top-right (e.g. CA12345AB, validated)", Sensitivity: "sensitive"},
				{ID: "cie.data_rilascio", Description: "Issue date (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "cie.data_scadenza", Description: "Expiry date (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "cie.comune_rilascio", Description: "Comune that issued the card", Sensitivity: "standard"},
			},
		},
		{
			Name:        "residenza",
			Description: "Registered residence (residenza anagrafica, as recorded in ANPR)",
			Fields: []SchemaField{
				{ID: "residenza.indirizzo", Description: "Street (via/piazza) without house number", Sensitivity: "sensitive"},
				{ID: "residenza.civico", Description: "House number, including interno/scala if any", Sensitivity: "sensitive"},
				{ID: "residenza.cap", Description: "CAP (5 digits, validated)", Sensitivity: "standard"},
				{ID: "residenza.comune", Description: "Comune of residence", Sensitivity: "standard"},
				{ID: "residenza.provincia", Description: "Provincia as 2-letter sigla (e.g. NA, validated)", Sensitivity: "standard"},
				{ID: "residenza.data_inizio", Description: "Date residence began (YYYY-MM-DD)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "domicilio",
			Description: "Domicile, only if different from residenza",
			Fields: []SchemaField{
				{ID: "domicilio.indirizzo", Description: "Street (via/piazza) without house number", Sensitivity: "sensitive"},
				{ID: "domicilio.civico", Description: "House number, including interno/scala if any", Sensitivity: "sensitive"},
				{ID: "domicilio.cap", Description: "CAP (5 digits, validated)", Sensitivity: "standard"},
				{ID: "domicilio.comune", Description: "Comune of domicile", Sensitivity: "standard"},
				{ID: "domicilio.provincia", Description: "Provincia as 2-letter sigla (e.g. NA, validated)", Sensitivity: "standard"},
			},
		},
		{
			Name: "nucleo_familiare",
			Description: "Household (stato di famiglia). Add one set of fields per member as membro<N>_<campo>, " +
				"e.g. nucleo_familiare.membro1_nome, membro1_cognome, membro1_codice_fiscale (validated), " +
				"membro1_data_nascita, membro1_relazione (coniuge, figlio/a, ...)",
			Fields: []SchemaField{
				{ID: "nucleo_familiare.numero_componenti", Description: "Number of people in the household, including you", Sensitivity: "standard"},
				{ID: "nucleo_familiare.intestatario_scheda", Description: "Whether you are the intestatario della scheda anagrafica (si/no)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "business",
			Description: "Italian business or self-employment details",
			Fields: []SchemaField{
				{ID: "business.partita_iva", Description: "Partita IVA (11 digits, validated; IT prefix stripped)", Sensitivity: "standard"},
				{ID: "business.denominazione", Description: "Business name or ditta individuale name", Sensitivity: "standard"},
				{ID: "business.codice_fiscale", Description: "Business codice fiscale if different from the partita IVA (validated)", Sensitivity: "standard"},
				{ID: "business.codice_ateco", Description: "Primary ATECO activity code (e.g. 62.01.00)", Sensitivity: "standard"},
				{ID: "business.regime_fiscale", Description: "Tax regime (e.g. forfettario, ordinario)", Sensitivity: "standard"},
				{ID: "business.pec", Description: "PEC (certified email) address", Sensitivity: "standard"},
				{ID: "business.rea", Description: "REA number (e.g. NA-123456)", Sensitivity: "standard"},
				{ID: "business.data_apertura", Description: "Date the partita IVA was opened (YYYY-MM-DD)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "addresses",
			Description: "Physical addresses",
			Fields: []SchemaField{
				{ID: "addresses.home_street", Description: "Home street address", Sensitivity: "sensitive"},
				{ID: "addresses.home_city", Description: "Home city", Sensitivity: "standard"},
				{ID: "addresses.home_state", Description: "Home state or province", Sensitivity: "standard"},
				{ID: "addresses.home_zip", Description: "Home ZIP or postal code", Sensitivity: "standard"},
				{ID: "addresses.home_country", Description: "Home country code (e.g. US)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "financial",
			Description: "Financial and tax information",
			Fields: []SchemaField{
				{ID: "financial.filing_status", Description: "Tax filing status", Sensitivity: "sensitive"},
				{ID: "financial.ssn", Description: "Social Security Number", Sensitivity: "critical"},
			},
		},
		{
			Name:        "payment",
			Description: "Payment card details",
			Fields: []SchemaField{
				{ID: "payment.card_number", Description: "Payment card number", Sensitivity: "critical"},
				{ID: "payment.card_expiry", Description: "Card expiration date", Sensitivity: "critical"},
				{ID: "payment.cardholder_name", Description: "Name on payment card", Sensitivity: "critical"},
				{ID: "payment.card_brand", Description: "Card brand (e.g. Visa, Mastercard)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "preferences",
			Description: "User preferences",
			Fields: []SchemaField{
				{ID: "preferences.timezone", Description: "Preferred timezone (e.g. America/New_York)", Sensitivity: "public"},
				{ID: "preferences.language", Description: "Preferred language (e.g. en)", Sensitivity: "public"},
			},
		},
		{
			Name:        "employment",
			Description: "Employment information",
			Fields: []SchemaField{
				{ID: "employment.employer", Description: "Current employer name", Sensitivity: "standard"},
				{ID: "employment.title", Description: "Job title", Sensitivity: "standard"},
			},
		},
		{
			Name:        "medical",
			Description: "Medical information (user-defined fields)",
			Fields:      []SchemaField{},
		},
		{
			Name:        "documents",
			Description: "Document references (user-defined fields)",
			Fields:      []SchemaField{},
		},
	},
}

var schemaIndex map[string]*SchemaField

func init() {
	schemaIndex = make(map[string]*SchemaField)
	for i := range RecommendedSchema.Categories {
		for j := range RecommendedSchema.Categories[i].Fields {
			f := &RecommendedSchema.Categories[i].Fields[j]
			schemaIndex[f.ID] = f
		}
	}
}

// IsCanonicalField returns true if the field ID is in the recommended schema.
func IsCanonicalField(id string) bool {
	_, ok := schemaIndex[id]
	return ok
}

// GetSchemaField returns the schema field for a canonical ID, or nil.
func GetSchemaField(id string) *SchemaField {
	return schemaIndex[id]
}

// DefaultSensitivity returns the schema default sensitivity for a field ID,
// or "standard" if the field is not in the schema. Non-canonical personal
// codice fiscale fields (e.g. nucleo_familiare.membro1_codice_fiscale)
// default to "sensitive".
func DefaultSensitivity(id string) string {
	if f, ok := schemaIndex[id]; ok {
		return f.Sensitivity
	}
	if parts := strings.SplitN(strings.ToLower(id), ".", 2); len(parts) == 2 && hasFieldSuffix(parts[1], "codice_fiscale") {
		return "sensitive"
	}
	return "standard"
}
