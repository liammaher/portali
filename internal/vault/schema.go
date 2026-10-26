package vault

import "strings"

// SchemaField describes a recommended field in the vault schema.
type SchemaField struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Sensitivity string `json:"sensitivity"`
	// For is "owner" or "member" when only the vault's owner or only a
	// family member (with a prefix) can have the field.
	For string `json:"for,omitempty"`
}

// Category scopes: who can have a category's fields.
const (
	ScopePerson = "person" // the owner and every family member (leo:cie.numero)
	ScopeOwner  = "owner"  // the household or the owner's own procedures only
)

// SchemaCategory groups recommended fields under a category.
type SchemaCategory struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Scope       string        `json:"scope"`
	Fields      []SchemaField `json:"fields"`
}

// Schema is the full recommended schema for the vault.
type Schema struct {
	Version    string           `json:"version"`
	Categories []SchemaCategory `json:"categories"`
}

// RecommendedSchema is the canonical schema that agents can discover.
var RecommendedSchema = Schema{
	Version: "10",
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
				{ID: "identity.birth_comune", Description: "Comune of birth; if born abroad, the place as written on your documents (and the country in identity.birth_country)", Sensitivity: "standard"},
				{ID: "identity.birth_province", Description: "Provincia of birth as 2-letter sigla (e.g. NA), or EE if born abroad", Sensitivity: "standard"},
				{ID: "identity.birth_country", Description: "Country of birth, only if born outside Italy (e.g. Regno Unito); forms ask for it as 'stato estero'", Sensitivity: "standard"},
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
				{ID: "spid.provider", Description: "Identity provider: " + spidProviderList + " (validated). Lower it to standard so agents can pick it at a SPID login", Sensitivity: "personal"},
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
				{ID: "residenza.civico", Description: "House number (numero civico) only, e.g. 544; scala or esponente like 12/B may follow", Sensitivity: "sensitive"},
				{ID: "residenza.interno", Description: "Flat number (interno), e.g. 13; 'int. 13' is saved as 13", Sensitivity: "standard"},
				{ID: "residenza.cap", Description: "CAP (5 digits, validated)", Sensitivity: "standard"},
				{ID: "residenza.comune", Description: "Comune of residence", Sensitivity: "standard"},
				{ID: "residenza.provincia", Description: "Provincia as 2-letter sigla (e.g. NA; the format is checked, not the list)", Sensitivity: "standard"},
				{ID: "residenza.data_inizio", Description: "Date residence began (YYYY-MM-DD)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "domicilio",
			Description: "Domicile, only if different from residenza",
			Fields: []SchemaField{
				{ID: "domicilio.indirizzo", Description: "Street (via/piazza) without house number", Sensitivity: "sensitive"},
				{ID: "domicilio.civico", Description: "House number (numero civico) only, e.g. 544; scala or esponente like 12/B may follow", Sensitivity: "sensitive"},
				{ID: "domicilio.interno", Description: "Flat number (interno), e.g. 13; 'int. 13' is saved as 13", Sensitivity: "standard"},
				{ID: "domicilio.cap", Description: "CAP (5 digits, validated)", Sensitivity: "standard"},
				{ID: "domicilio.comune", Description: "Comune of domicile", Sensitivity: "standard"},
				{ID: "domicilio.provincia", Description: "Provincia as 2-letter sigla (e.g. NA; the format is checked, not the list)", Sensitivity: "standard"},
			},
		},
		{
			Name: "abitazione",
			Description: "The home you live in now (the unit at your residenza or domicilio; the address stays there). " +
				"Catasto data is on the visura catastale and is needed for TARI, RLI registration and ISEE. " +
				"Attach the visura and APE with 'pvault doc add abitazione <file>'.",
			Fields: []SchemaField{
				{ID: "abitazione.titolo", Description: "How you hold it: proprietario, locatario, comodatario, usufruttuario, ospite or altro (validated)", Sensitivity: "standard"},
				{ID: "abitazione.data_inizio", Description: "Date you started living there (YYYY-MM-DD); the 'a decorrere dal' date on TARI forms", Sensitivity: "standard"},
				{ID: "abitazione.catasto_sezione", Description: "Sezione urbana from the visura, if any", Sensitivity: "standard"},
				{ID: "abitazione.catasto_foglio", Description: "Foglio from the visura (digits, validated)", Sensitivity: "standard"},
				{ID: "abitazione.catasto_particella", Description: "Particella (mappale) from the visura (validated)", Sensitivity: "standard"},
				{ID: "abitazione.catasto_subalterno", Description: "Subalterno from the visura, exactly as written (digits, validated)", Sensitivity: "standard"},
				{ID: "abitazione.catasto_categoria", Description: "Categoria catastale, e.g. A/2 or A/3 (validated; a2 becomes A/2)", Sensitivity: "standard"},
				{ID: "abitazione.catasto_rendita", Description: "Rendita catastale in euro (e.g. 512.30)", Sensitivity: "standard"},
				{ID: "abitazione.superficie_calpestabile_mq", Description: "Walkable floor area in m² excluding balconies and terraces; the area TARI uses", Sensitivity: "standard"},
				{ID: "abitazione.superficie_catastale_mq", Description: "Superficie catastale in m² from the visura", Sensitivity: "standard"},
				{ID: "abitazione.vani", Description: "Consistenza in vani from the visura (e.g. 4.5)", Sensitivity: "standard"},
				{ID: "abitazione.piano", Description: "Floor (e.g. terra, rialzato, 3)", Sensitivity: "standard"},
				{ID: "abitazione.classe_energetica", Description: "Energy class from the APE: A4, A3, A2, A1, B, C, D, E, F or G (validated)", Sensitivity: "standard"},
			},
		},
		{
			Name: "locazione",
			Description: "Your rental contract, if you rent. Registration details (estremi di registrazione) are on the RLI receipt " +
				"and are asked for by utility transfers and tax deductions. Attach the contract and receipt with 'pvault doc add locazione <file>'.",
			Fields: []SchemaField{
				{ID: "locazione.tipo_contratto", Description: "Contract type (e.g. 4+4 libero, 3+2 concordato, transitorio, studenti)", Sensitivity: "standard"},
				{ID: "locazione.data_stipula", Description: "Date the contract was signed (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "locazione.data_decorrenza", Description: "Date the contract starts (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "locazione.data_scadenza", Description: "Date the current term ends (YYYY-MM-DD); tracked by 'pvault expiring'", Sensitivity: "standard"},
				{ID: "locazione.preavviso_disdetta_mesi", Description: "Months of notice needed to leave (whole number)", Sensitivity: "standard"},
				{ID: "locazione.canone_mensile", Description: "Monthly rent in euro (e.g. 750.00)", Sensitivity: "sensitive"},
				{ID: "locazione.spese_condominiali_mensili", Description: "Monthly condominium charges in euro", Sensitivity: "sensitive"},
				{ID: "locazione.deposito_cauzionale", Description: "Security deposit in euro", Sensitivity: "sensitive"},
				{ID: "locazione.cedolare_secca", Description: "Whether the landlord chose cedolare secca (si/no)", Sensitivity: "standard"},
				{ID: "locazione.registrazione_codice_identificativo", Description: "Codice identificativo del contratto from the RLI receipt (validated)", Sensitivity: "standard"},
				{ID: "locazione.data_registrazione", Description: "Registration date from the RLI receipt (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "locazione.registrazione_serie", Description: "Registration serie (e.g. 3T)", Sensitivity: "standard"},
				{ID: "locazione.registrazione_numero", Description: "Registration numero (and sottonumero, if any)", Sensitivity: "standard"},
				{ID: "locazione.registrazione_ufficio", Description: "Agenzia delle Entrate office code (e.g. TNM)", Sensitivity: "standard"},
				{ID: "locazione.locatore_nome", Description: "Landlord's name (person or company)", Sensitivity: "sensitive"},
				{ID: "locazione.locatore_codice_fiscale", Description: "Landlord's codice fiscale (validated)", Sensitivity: "sensitive"},
				{ID: "locazione.locatore_iban", Description: "IBAN you pay the rent to (validated; spaces removed)", Sensitivity: "sensitive"},
				{ID: "locazione.locatore_email", Description: "Landlord's email", Sensitivity: "sensitive"},
				{ID: "locazione.locatore_pec", Description: "Landlord's PEC", Sensitivity: "sensitive"},
				{ID: "locazione.locatore_telefono", Description: "Landlord's phone", Sensitivity: "sensitive"},
				{ID: "locazione.amministratore_condominio", Description: "Building manager's name and contact", Sensitivity: "standard"},
			},
		},
		{
			Name:        "utenze",
			Description: "Utility supplies at your home. The POD and PDR identify the meters and are needed for a voltura or subentro.",
			Fields: []SchemaField{
				{ID: "utenze.luce_pod", Description: "Electricity POD (e.g. IT001E12345678, validated)", Sensitivity: "standard"},
				{ID: "utenze.luce_fornitore", Description: "Electricity supplier", Sensitivity: "standard"},
				{ID: "utenze.luce_codice_cliente", Description: "Electricity customer code", Sensitivity: "sensitive"},
				{ID: "utenze.luce_intestatario", Description: "Who the electricity contract is in the name of", Sensitivity: "standard"},
				{ID: "utenze.gas_pdr", Description: "Gas PDR (14 digits, validated)", Sensitivity: "standard"},
				{ID: "utenze.gas_fornitore", Description: "Gas supplier", Sensitivity: "standard"},
				{ID: "utenze.gas_codice_cliente", Description: "Gas customer code", Sensitivity: "sensitive"},
				{ID: "utenze.gas_intestatario", Description: "Who the gas contract is in the name of", Sensitivity: "standard"},
				{ID: "utenze.acqua_fornitore", Description: "Water supplier (e.g. ABC Napoli)", Sensitivity: "standard"},
				{ID: "utenze.acqua_codice_cliente", Description: "Water customer or utenza code", Sensitivity: "sensitive"},
				{ID: "utenze.acqua_intestatario", Description: "Who the water contract is in the name of", Sensitivity: "standard"},
				{ID: "utenze.internet_fornitore", Description: "Internet provider", Sensitivity: "standard"},
				{ID: "utenze.internet_codice_cliente", Description: "Internet customer code", Sensitivity: "sensitive"},
			},
		},
		{
			Name:        "tari",
			Description: "Your TARI (waste tax) position with the Comune. Attach the filed declaration with 'pvault doc add tari <file>'.",
			Fields: []SchemaField{
				{ID: "tari.codice_contribuente", Description: "Codice contribuente assigned by the Comune (Cod.Contr. on the forms and payment notices)", Sensitivity: "standard"},
				{ID: "tari.data_dichiarazione", Description: "Date you filed the TARI declaration (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "tari.protocollo", Description: "Protocol number the Comune gave the declaration", Sensitivity: "standard"},
				{ID: "tari.superficie_tassata_mq", Description: "Area the Comune taxes, in m², from the payment notice", Sensitivity: "standard"},
				{ID: "tari.componenti_tassati", Description: "Number of occupants the Comune taxes, from the payment notice", Sensitivity: "standard"},
			},
		},
		{
			Name:        "pratiche",
			Description: praticheDescription(),
			Fields:      []SchemaField{},
		},
		{
			Name:        "famiglia",
			Description: famigliaDescription(),
			Fields: []SchemaField{
				{ID: "famiglia.intestatario_scheda", Description: "Who is the intestatario della scheda anagrafica: io (you) or a member's slug (validated)", Sensitivity: "standard", For: "owner"},
				{ID: "famiglia.relazione", Description: "How the member relates to you: " + strings.Join(relazioneKeys(), ", ") + " (validated; moglie, figlia, madre... accepted)", Sensitivity: "standard", For: "member"},
				{ID: "famiglia.stato_famiglia", Description: "Whether the member is in your stato di famiglia (famiglia anagrafica): si or no", Sensitivity: "standard", For: "member"},
				{ID: "famiglia.a_carico_percentuale", Description: "Share of the member you claim as an IRPEF dependant: 0, 50 or 100", Sensitivity: "sensitive", For: "member"},
				{ID: "famiglia.genitori", Description: "For a child: who holds responsabilità genitoriale, io and/or members' slugs (e.g. io,giulia)", Sensitivity: "standard", For: "member"},
				{ID: "famiglia.rappresentanza", Description: "How you can act for an adult member: " + strings.Join(rappresentanze, ", "), Sensitivity: "standard", For: "member"},
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
				{ID: "business.qualita_dichiarante", Description: "Your role when you sign for the business: titolare or legale_rappresentante (validated)", Sensitivity: "standard"},
				{ID: "business.email", Description: "Business email address (not the PEC)", Sensitivity: "standard"},
				{ID: "business.descrizione_attivita", Description: "Short description of the activity carried on, as on SCIA and TARI forms", Sensitivity: "standard"},
				{ID: "business.sede_legale_indirizzo", Description: "Registered office (sede legale): street (via/piazza) without number", Sensitivity: "standard"},
				{ID: "business.sede_legale_civico", Description: "Registered office: street number", Sensitivity: "standard"},
				{ID: "business.sede_legale_cap", Description: "Registered office: CAP (5 digits, validated)", Sensitivity: "standard"},
				{ID: "business.sede_legale_comune", Description: "Registered office: comune", Sensitivity: "standard"},
				{ID: "business.sede_legale_provincia", Description: "Registered office: provincia as 2-letter sigla (e.g. NA; the format is checked, not the list)", Sensitivity: "standard"},
			},
		},
		{
			Name: "locale",
			Description: "The business premises (locali) where the activity is carried on, one unit; like abitazione for the home, " +
				"but with its own address. Catasto data is on the visura catastale. Attach the visura and the lease with 'pvault doc add locale <file>'.",
			Fields: []SchemaField{
				{ID: "locale.indirizzo", Description: "Street (via/piazza) of the premises, without number", Sensitivity: "standard"},
				{ID: "locale.civico", Description: "Street number of the premises", Sensitivity: "standard"},
				{ID: "locale.interno", Description: "Unit number (interno), if any; 'int. 3' is saved as 3", Sensitivity: "standard"},
				{ID: "locale.cap", Description: "CAP of the premises (5 digits, validated)", Sensitivity: "standard"},
				{ID: "locale.comune", Description: "Comune of the premises", Sensitivity: "standard"},
				{ID: "locale.provincia", Description: "Provincia of the premises as 2-letter sigla (e.g. NA; the format is checked, not the list)", Sensitivity: "standard"},
				{ID: "locale.titolo", Description: "How the business holds the premises: proprietario, locatario, comodatario, usufruttuario or altro (validated)", Sensitivity: "standard"},
				{ID: "locale.data_inizio", Description: "Date occupation of the premises started (YYYY-MM-DD)", Sensitivity: "standard"},
				{ID: "locale.catasto_sezione", Description: "Sezione urbana from the visura, if any", Sensitivity: "standard"},
				{ID: "locale.catasto_foglio", Description: "Foglio from the visura (digits, validated)", Sensitivity: "standard"},
				{ID: "locale.catasto_particella", Description: "Particella (mappale) from the visura (validated)", Sensitivity: "standard"},
				{ID: "locale.catasto_subalterno", Description: "Subalterno from the visura, exactly as written (digits, validated)", Sensitivity: "standard"},
				{ID: "locale.catasto_categoria", Description: "Categoria catastale, e.g. C/1 for a shop (validated; c1 becomes C/1)", Sensitivity: "standard"},
				{ID: "locale.superficie_mq", Description: "Surface used for the activity, in m²", Sensitivity: "standard"},
				{ID: "locale.destinazione_uso", Description: "Lawful use of the premises (e.g. negozio, ufficio, laboratorio, magazzino)", Sensitivity: "standard"},
			},
		},
		{
			Name:        "permesso_soggiorno",
			Description: "Permesso / documento di soggiorno (non-EU residents, incl. UK citizens under the Withdrawal Agreement; not needed when identity.citizenship is italiana). Attach scans with 'pvault doc add permesso_soggiorno <file>'.",
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
				{ID: "preferences.accesso_pa", Description: "How you log in to government portals by default: spid or cie (validated). With spid, agents use spid.provider", Sensitivity: "standard"},
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
		c := &RecommendedSchema.Categories[i]
		if c.Scope = ScopePerson; IsOwnerOnlyCategory(c.Name) {
			c.Scope = ScopeOwner
		}
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
// codice fiscale fields (e.g. custom.coniuge_codice_fiscale) default to
// "sensitive". A member's field defaults like the owner's.
func DefaultSensitivity(id string) string {
	_, id = SplitMember(id)
	// Validation ignores case, so the default must too (financial.SSN is
	// still critical).
	if f, ok := schemaIndex[strings.ToLower(id)]; ok {
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
