package vault

import "strings"

// The procura category records a delega or procura another person gave the
// vault's principal: a client's procura speciale for a SUE or SUAP filing,
// the Agenzia delle Entrate delega unica, an INPS delega, or a parent's
// delega in a personal vault. It is always on the person who gave it
// (rossi:procura.ente), with the signed document in rossi:procura; a second
// one goes in procura2. Its data_scadenza shows in 'pvault expiring'.
//
// Person: rossi:procura.ente, oggetto, data_conferimento, data_scadenza

// isPersonOnlyCategory reports a category only other people can have: the
// procura they gave, and its numbered copies (procura2).
func isPersonOnlyCategory(category string) bool {
	c := schemaCategoryOf(category)
	return c != nil && c.Name == "procura"
}

// procuraDescription documents the category in the schema.
func procuraDescription() string {
	return "A delega or procura a person gave you (or, in a business vault, the business): e.g. a client's procura speciale " +
		"for a SUE/SUAP filing, the Agenzia delle Entrate delega unica, an INPS delega. Only on another person, " +
		"e.g. rossi:procura.ente; a second one goes in procura2. Attach the signed procura with " +
		"'pvault doc add rossi:procura <file>'. data_scadenza is tracked by 'pvault expiring'. " +
		"Never store the person's SPID or CIE credentials instead: act with your own and the procura."
}

// procuraFields are the schema's procura fields.
func procuraFields() []SchemaField {
	fields := []SchemaField{
		{ID: "procura.ente", Description: "Who the procura is for: the ente or portal (e.g. SUE Comune di Napoli, Agenzia delle Entrate, INPS)"},
		{ID: "procura.oggetto", Description: "What it allows, as written in it (e.g. presentazione CILA per l'immobile di via Roma 1)"},
		{ID: "procura.data_conferimento", Description: "Date it was given or signed (YYYY-MM-DD)"},
		{ID: "procura.data_scadenza", Description: "Date it ends (YYYY-MM-DD); tracked by 'pvault expiring'"},
	}
	for i := range fields {
		fields[i].Sensitivity = "standard"
		fields[i].For = ForPerson
	}
	return fields
}

// errPersonOnlyCategory is the message for procura without a person.
func errPersonOnlyCategory(category string) string {
	base := strings.ToLower(category)
	return base + " is a delega another person gave you: store it on them, e.g. rossi:" + base + ".ente"
}
