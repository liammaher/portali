package vault

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
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

// praticaCanaleAliases are the channel types of a pratiche-napoli entry that
// name a canale differently, so its type can be saved as it is. Its phone
// channel is for questions, never a way to file, so it is no canale.
var praticaCanaleAliases = map[string]string{"portal": "portale", "desk": "sportello", "post": "posta"}

// isPraticheCategory reports the category that tracks procedures, or a
// numbered copy of it, for the rules: a copy is owner-only like pratiche,
// and NormalizeAndValidate refuses it (oneOnly).
func isPraticheCategory(category string) bool {
	c := schemaCategoryOf(category)
	return c != nil && c.Name == "pratiche"
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

// praticaField is splitPraticaField for a field in the pratiche category;
// ok is false for any other category.
func praticaField(category, name string) (slug, campo string, ok bool) {
	if category != "pratiche" {
		return "", "", false
	}
	return splitPraticaField(name)
}

// praticaOfID is praticaField for a field ID.
func praticaOfID(id string) (slug, campo string, ok bool) {
	category, name, _ := strings.Cut(id, ".")
	return praticaField(category, name)
}

// praticaState is what a pratica's stato and persona fields say.
type praticaState struct {
	stato    string    // "" without a stato field
	closedAt time.Time // when the stato closed (FieldInfo.ClosedAt)
	persone  []string  // whom persona names, io included
	hidden   bool      // a stato or persona field the caller can't see
}

func (p *praticaState) closed() bool { return isClosedStato(p.stato) }

// praticheIn collects the pratiche whose stato or persona ctx holds, by slug.
// hidden marks those with a field allow rejects.
func praticheIn(ctx *ContextBundle, allow FieldFilter) map[string]*praticaState {
	out := map[string]*praticaState{}
	for _, f := range ctx.Categories["pratiche"] {
		slug, campo, ok := praticaField(f.Category, f.FieldName)
		if !ok || (campo != "stato" && campo != "persona") {
			continue
		}
		p := out[slug]
		if p == nil {
			p = &praticaState{}
			out[slug] = p
		}
		p.hidden = p.hidden || (allow != nil && !allow(f.ID, f.Sensitivity))
		if campo == "stato" {
			p.stato, p.closedAt = f.Value, f.ClosedAt
		} else {
			p.persone = PersonRefs(f.Value)
		}
	}
	return out
}

// praticheDescription documents the naming convention in the schema.
func praticheDescription() string {
	return "Procedures in progress with an ente, one set of fields per pratica as <slug>_<campo>. " +
		"The slug is lower-case words joined by hyphens, e.g. pratiche.tari-iscrizione-2026_stato. " +
		"Campi: " + strings.Join(praticaCampi, ", ") + ". " +
		"stato is one of " + strings.Join(praticaStati, ", ") + "; " +
		"canale is one of " + strings.Join(praticaCanali, ", ") + " (a pratiche-napoli channel type is accepted as it is: " + canaleAliasList() + "). " +
		"data_scadenza is the deadline, tracked by 'pvault expiring' until stato is " + orList(praticaStatiChiusi) + ". " +
		"playbook names the procedure's playbook file. " +
		"persona is who it is for: io (the vault's own, the default) or another person's slug (a family member, or a business vault's client). Attach submitted files with 'pvault doc add pratiche <file>'."
}

// canaleAliasList says each alias as "portal is portale", in a fixed order.
func canaleAliasList() string {
	var out []string
	for _, alias := range slices.Sorted(maps.Keys(praticaCanaleAliases)) {
		out = append(out, alias+" is "+praticaCanaleAliases[alias])
	}
	return strings.Join(out, ", ")
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
		if canale, ok := praticaCanaleAliases[normalizeChoice(value)]; ok {
			value = canale
		}
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
