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
	Version: "5",
	Categories: []SchemaCategory{
		{
			Name:        "identity",
			Description: "Personal identity information",
			Fields: []SchemaField{
				{ID: "identity.first_name", Description: "First/given name", Sensitivity: "standard"},
				{ID: "identity.last_name", Description: "Last/family name", Sensitivity: "standard"},
				{ID: "identity.full_name", Description: "Full display name", Sensitivity: "standard"},
				{ID: "identity.email", Description: "Primary email address", Sensitivity: "standard"},
				{ID: "identity.phone", Description: "Mobile number with country code (e.g. +39 333 123 4567)", Sensitivity: "sensitive"},
				{ID: "identity.pec", Description: "Personal PEC (posta elettronica certificata) address", Sensitivity: "standard"},
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
			Description: "Carta d'identità elettronica details. The PIN and PUK can only be stored as personal (never readable by agents).",
			Fields: []SchemaField{
				{ID: "cie.numero", Description: "CIE number printed top-right (e.g. CA12345AB, validated)", Sensitivity: "sensitive"},
				{ID: "cie.data_rilascio", Description: "Issue date (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "cie.data_scadenza", Description: "Expiry date (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "cie.comune_rilascio", Description: "Comune that issued the card", Sensitivity: "standard"},
				{ID: "cie.pin", Description: "CIE PIN (8 digits) — personal only", Sensitivity: "personal"},
				{ID: "cie.puk", Description: "CIE PUK (8 digits) — personal only", Sensitivity: "personal"},
			},
		},
		{
			Name:        "spid",
			Description: "Your SPID identity. Everything here is personal by default: only you can see it, never an agent. Lower provider/level/renewal to standard if you want agents to see them.",
			Fields: []SchemaField{
				{ID: "spid.provider", Description: "Identity provider (e.g. PosteID, Aruba, InfoCert, Namirial, Sielte, Lepida)", Sensitivity: "personal"},
				{ID: "spid.livello", Description: "Highest active level (1, 2 or 3)", Sensitivity: "personal"},
				{ID: "spid.username", Description: "Login username (usually email or codice fiscale)", Sensitivity: "personal"},
				{ID: "spid.password", Description: "Login password", Sensitivity: "personal"},
				{ID: "spid.data_scadenza", Description: "Next renewal/payment date (YYYY-MM-DD); tracked by 'pvault expiring'", Sensitivity: "personal"},
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
			Name:        "permesso_soggiorno",
			Description: "Permesso / documento di soggiorno (non-EU residents, incl. UK citizens under the Withdrawal Agreement). Attach scans with 'pvault doc add permesso_soggiorno <file>'.",
			Fields: []SchemaField{
				{ID: "permesso_soggiorno.numero", Description: "Document number printed on the card", Sensitivity: "sensitive"},
				{ID: "permesso_soggiorno.tipo", Description: "Type or reason (e.g. Art. 50 TUE, lavoro subordinato, soggiornante di lungo periodo)", Sensitivity: "standard"},
				{ID: "permesso_soggiorno.data_rilascio", Description: "Issue date (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "permesso_soggiorno.data_scadenza", Description: "Expiry date (YYYY-MM-DD); tracked by 'pvault expiring'", Sensitivity: "standard"},
				{ID: "permesso_soggiorno.questura_rilascio", Description: "Issuing Questura (e.g. Napoli)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "passaporto",
			Description: "Passport (Italian or foreign)",
			Fields: []SchemaField{
				{ID: "passaporto.numero", Description: "Passport number", Sensitivity: "sensitive"},
				{ID: "passaporto.paese_rilascio", Description: "Issuing country (e.g. Regno Unito)", Sensitivity: "standard"},
				{ID: "passaporto.data_rilascio", Description: "Issue date (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "passaporto.data_scadenza", Description: "Expiry date (YYYY-MM-DD); tracked by 'pvault expiring'", Sensitivity: "standard"},
				{ID: "passaporto.autorita_rilascio", Description: "Issuing authority (e.g. Questura di Napoli, HM Passport Office)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "tessera_sanitaria",
			Description: "Tessera sanitaria / TEAM card (also shows the codice fiscale)",
			Fields: []SchemaField{
				{ID: "tessera_sanitaria.numero", Description: "Card identification number on the back", Sensitivity: "sensitive"},
				{ID: "tessera_sanitaria.data_scadenza", Description: "Expiry date (YYYY-MM-DD); tracked by 'pvault expiring'", Sensitivity: "standard"},
				{ID: "tessera_sanitaria.regione", Description: "Region / ASL of enrolment in the SSN", Sensitivity: "standard"},
				{ID: "tessera_sanitaria.medico_base", Description: "Medico di base (GP) name", Sensitivity: "standard"},
			},
		},
		{
			Name:        "patente",
			Description: "Driving licence (Italian or foreign)",
			Fields: []SchemaField{
				{ID: "patente.numero", Description: "Licence number", Sensitivity: "sensitive"},
				{ID: "patente.categorie", Description: "Categories (e.g. B, A2)", Sensitivity: "standard"},
				{ID: "patente.paese_rilascio", Description: "Issuing country", Sensitivity: "standard"},
				{ID: "patente.data_rilascio", Description: "Issue date (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "patente.data_scadenza", Description: "Expiry date (YYYY-MM-DD); tracked by 'pvault expiring'", Sensitivity: "standard"},
				{ID: "patente.autorita_rilascio", Description: "Issuing authority (e.g. MIT-UCO, DVLA)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "addresses",
			Description: "Other or foreign addresses (e.g. a previous address abroad). For your Italian address use residenza and domicilio.",
			Fields: []SchemaField{
				{ID: "addresses.home_street", Description: "Home street address", Sensitivity: "sensitive"},
				{ID: "addresses.home_city", Description: "Home city", Sensitivity: "standard"},
				{ID: "addresses.home_state", Description: "Region, county or state", Sensitivity: "standard"},
				{ID: "addresses.home_zip", Description: "Postal code", Sensitivity: "standard"},
				{ID: "addresses.home_country", Description: "Country code (e.g. GB)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "financial",
			Description: "Financial and tax information",
			Fields: []SchemaField{
				{ID: "financial.iban", Description: "IBAN for refunds (730, INPS) and payments (validated; spaces removed)", Sensitivity: "sensitive"},
				{ID: "financial.filing_status", Description: "US tax filing status (US taxpayers only)", Sensitivity: "sensitive"},
				{ID: "financial.ssn", Description: "US Social Security Number (US taxpayers only)", Sensitivity: "critical"},
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
				{ID: "preferences.timezone", Description: "Preferred timezone (e.g. Europe/Rome)", Sensitivity: "public"},
				{ID: "preferences.language", Description: "Preferred language (e.g. it, en)", Sensitivity: "public"},
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
	if parts := strings.SplitN(id, ".", 2); IsCredentialField(id) || (len(parts) == 2 && IsPersonalCategory(parts[0])) {
		return TierPersonal
	}
	if parts := strings.SplitN(strings.ToLower(id), ".", 2); len(parts) == 2 && hasFieldSuffix(parts[1], "codice_fiscale") {
		return "sensitive"
	}
	return "standard"
}
