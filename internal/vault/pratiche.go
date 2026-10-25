package vault

import (
	"regexp"
	"slices"
	"strings"
)

// Pratiche are the procedures you have in progress with an ente (a TARI
// declaration, a residence change, a renewal). Each one is a set of fields
// <slug>_<campo> in the pratiche category, e.g. pratiche.tari-iscrizione-2026_stato.
// The slug is lower-case words joined by hyphens, so the first underscore
// always separates it from the campo.

var rePraticaSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// praticaCampi are the fields each pratica can have, in display order.
var praticaCampi = []string{
	"titolo", "ente", "stato", "canale", "destinatario", "protocollo",
	"data_invio", "data_scadenza", "data_esito", "importo", "data_pagamento",
	"prossimo_passo", "playbook", "note",
}

// praticaStati are the stages of a pratica, in order.
var praticaStati = []string{
	"da_fare", "in_preparazione", "pronta", "inviata", "in_attesa",
	"completata", "respinta", "annullata",
}

// IsClosedStato reports the stati that end a pratica; its deadline no longer
// shows in 'pvault expiring'. Values saved before validation (e.g.
// "Completata ") count too.
func IsClosedStato(stato string) bool {
	switch normalizeChoice(stato) {
	case "completata", "respinta", "annullata":
		return true
	}
	return false
}

// praticaCanali are the ways a pratica can be submitted.
var praticaCanali = []string{"portale", "pec", "email", "sportello", "posta", "app"}

// IsPraticheCategory reports the category that tracks procedures.
func IsPraticheCategory(category string) bool {
	return strings.EqualFold(category, "pratiche")
}

// SplitPraticaField splits a pratiche field name into its slug and campo,
// e.g. "tari-iscrizione-2026_data_invio" into "tari-iscrizione-2026" and
// "data_invio". ok is false when the name doesn't follow the convention,
// including any upper-case letter.
func SplitPraticaField(name string) (slug, campo string, ok bool) {
	slug, campo, found := strings.Cut(name, "_")
	if !found || !rePraticaSlug.MatchString(slug) || !slices.Contains(praticaCampi, campo) {
		return "", "", false
	}
	return slug, campo, true
}

// praticaField is SplitPraticaField for a field in the pratiche category;
// ok is false for any other category.
func praticaField(category, name string) (slug, campo string, ok bool) {
	if !IsPraticheCategory(category) {
		return "", "", false
	}
	return SplitPraticaField(name)
}

// praticheDescription documents the naming convention in the schema.
func praticheDescription() string {
	return "Procedures in progress with an ente, one set of fields per pratica as <slug>_<campo>. " +
		"The slug is lower-case words joined by hyphens, e.g. pratiche.tari-iscrizione-2026_stato. " +
		"Campi: " + strings.Join(praticaCampi, ", ") + ". " +
		"stato is one of " + strings.Join(praticaStati, ", ") + "; " +
		"canale is one of " + strings.Join(praticaCanali, ", ") + ". " +
		"data_scadenza is the deadline, tracked by 'pvault expiring' until stato is completata, respinta or annullata. " +
		"playbook names the procedure's playbook file. Attach submitted files with 'pvault doc add pratiche <file>'."
}

// normalizePratica applies the pratiche rules. handled is false outside the
// pratiche category, so the generic rules still apply there.
func normalizePratica(id, category, name, value string) (out string, handled bool, err error) {
	if !IsPraticheCategory(category) {
		return value, false, nil
	}
	fail := func(msg string) (string, bool, error) {
		return "", true, &ValidationError{Field: id, Msg: msg}
	}
	// name is lower-cased by NormalizeAndValidate; IDs are case-sensitive,
	// so TARI-2026_stato would be a second field for the same pratica.
	_, campo, ok := SplitPraticaField(name)
	if _, original, _ := strings.Cut(id, "."); original != name {
		ok = false
	}
	if !ok {
		return fail("pratiche fields are <slug>_<campo>: a slug of lower-case words joined by hyphens " +
			"(e.g. tari-iscrizione-2026), then one of " + strings.Join(praticaCampi, ", "))
	}

	switch campo {
	case "stato":
		return oneOf(value, praticaStati, fail, "stato")
	case "canale":
		return oneOf(value, praticaCanali, fail, "canale")
	case "importo":
		v, ok := normalizeEuro(value)
		if !ok {
			return fail("must be an amount in euro (e.g. 259, 259,00 or 1.250,50)")
		}
		return v, true, nil
	case "data_invio", "data_scadenza", "data_esito", "data_pagamento":
		d, err := NormalizeDate(value)
		if err != nil {
			return fail(err.Error())
		}
		return d, true, nil
	}
	return strings.TrimSpace(value), true, nil
}

// normalizeChoice lower-cases value and reads spaces and hyphens as
// underscores, so "In attesa" is in_attesa.
func normalizeChoice(value string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}

// oneOf normalizes value with normalizeChoice and checks it against allowed.
func oneOf(value string, allowed []string, fail func(string) (string, bool, error), what string) (string, bool, error) {
	v := normalizeChoice(value)
	if !slices.Contains(allowed, v) {
		return fail(what + " must be one of " + strings.Join(allowed, ", "))
	}
	return v, true, nil
}
