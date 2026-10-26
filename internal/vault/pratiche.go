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
	"prossimo_passo", "playbook", "note", "persona",
}

// praticaStati are the stages of a pratica, in order.
var praticaStati = append([]string{
	"da_fare", "in_preparazione", "pronta", "inviata", "in_attesa",
}, praticaStatiChiusi...)

// praticaStatiChiusi are the stati that end a pratica; its deadline no longer
// shows in 'pvault expiring'.
var praticaStatiChiusi = []string{"completata", "respinta", "annullata"}

// isClosedStato reports a stato in praticaStatiChiusi. Values saved before
// validation (e.g. "Completata ") count too.
func isClosedStato(stato string) bool {
	return slices.Contains(praticaStatiChiusi, normalizeChoice(stato))
}

// praticaCanali are the ways a pratica can be submitted.
var praticaCanali = []string{"portale", "pec", "email", "sportello", "posta", "app"}

// isPraticheCategory reports the category that tracks procedures.
func isPraticheCategory(category string) bool {
	return strings.EqualFold(category, "pratiche")
}

// splitPraticaField splits a pratiche field name into its slug and campo,
// e.g. "tari-iscrizione-2026_data_invio" into "tari-iscrizione-2026" and
// "data_invio". ok is false when the name doesn't follow the convention,
// including any upper-case letter.
func splitPraticaField(name string) (slug, campo string, ok bool) {
	slug, campo, found := strings.Cut(name, "_")
	if !found || !rePraticaSlug.MatchString(slug) || !slices.Contains(praticaCampi, campo) {
		return "", "", false
	}
	return slug, campo, true
}

// praticaField is splitPraticaField for a stored field in the pratiche
// category, ignoring case, so a row from before IDs had to be lower-case
// (pratiche.cila-roma_STATO) counts as the pratica it names; ok is false for
// any other category. Validation calls splitPraticaField itself, which
// refuses upper case.
func praticaField(category, name string) (slug, campo string, ok bool) {
	if !isPraticheCategory(category) {
		return "", "", false
	}
	return splitPraticaField(strings.ToLower(name))
}

// praticheDescription documents the naming convention in the schema.
func praticheDescription() string {
	return "Procedures in progress with an ente, one set of fields per pratica as <slug>_<campo>. " +
		"The slug is lower-case words joined by hyphens, e.g. pratiche.tari-iscrizione-2026_stato. " +
		"Campi: " + strings.Join(praticaCampi, ", ") + ". " +
		"stato is one of " + strings.Join(praticaStati, ", ") + "; " +
		"canale is one of " + strings.Join(praticaCanali, ", ") + ". " +
		"data_scadenza is the deadline, tracked by 'pvault expiring' until stato is " + orList(praticaStatiChiusi) + ". " +
		"playbook names the procedure's playbook file. " +
		"persona is who it is for: io (you, the default) or a family member's slug. Attach submitted files with 'pvault doc add pratiche <file>'."
}

// normalizePratica applies the pratiche rules. handled is false outside the
// pratiche category, so the generic rules still apply there.
func normalizePratica(id, category, name, value string) (out string, handled bool, err error) {
	if !isPraticheCategory(category) {
		return value, false, nil
	}
	fail := func(msg string) (string, bool, error) {
		return "", true, &ValidationError{Field: id, Msg: msg}
	}
	// name is lower-cased by NormalizeAndValidate, so split the original:
	// IDs are case-sensitive, and TARI-2026_stato would be a second field
	// for the same pratica.
	_, original, _ := strings.Cut(id, ".")
	_, campo, ok := splitPraticaField(original)
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
	case "persona":
		v, msg := normalizePersonRefs(value, "", 1)
		if msg != "" {
			return fail("persona " + msg)
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
