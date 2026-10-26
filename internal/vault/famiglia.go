package vault

import (
	"slices"
	"strings"
)

// The famiglia category records how household members relate to the vault's
// owner, the way ANPR records every relazione di parentela against the
// intestatario della scheda. Only facts are stored; which members count for
// an ISEE nucleo, a 730 or Assegno Unico is up to each procedure's playbook.
//
// Owner:  famiglia.intestatario_scheda (io or a member's slug)
// Member: leo:famiglia.relazione, stato_famiglia, a_carico_percentuale,
//         genitori, rappresentanza

// famigliaOwnerFields can only be stored without a member prefix,
// famigliaMemberFields only with one.
var (
	famigliaOwnerFields  = []string{"intestatario_scheda"}
	famigliaMemberFields = []string{"relazione", "stato_famiglia", "a_carico_percentuale", "genitori", "rappresentanza"}
)

// relazione is one way a member relates to the owner.
type relazione struct {
	key  string
	anpr string // code in ANPR Tabella 05 (relazione di parentela), "" if none
	// Labels for forms, by the member's sex (M, F) and when it's unknown.
	male, female, neutral string
}

// relazioni are the accepted relazioni, in the order the schema lists them.
var relazioni = []relazione{
	{"coniuge", "2", "marito", "moglie", "coniuge"},
	{"unito_civilmente", "28", "unito civilmente", "unita civilmente", "unito/a civilmente"},
	{"convivente", "23", "convivente", "convivente", "convivente"},
	{"figlio", "3", "figlio", "figlia", "figlio/a"},
	{"nipote", "4", "nipote", "nipote", "nipote"},
	{"genitore", "6", "padre", "madre", "genitore"},
	{"nonno", "7", "nonno", "nonna", "nonno/a"},
	{"fratello", "9", "fratello", "sorella", "fratello/sorella"},
	{"nipote_collaterale", "10", "nipote", "nipote", "nipote"},
	{"zio", "11", "zio", "zia", "zio/a"},
	{"cugino", "12", "cugino", "cugina", "cugino/a"},
	{"altro_parente", "13", "altro parente", "altro parente", "altro parente"},
	{"genero_nuora", "16", "genero", "nuora", "genero/nuora"},
	{"suocero", "17", "suocero", "suocera", "suocero/a"},
	{"cognato", "18", "cognato", "cognata", "cognato/a"},
	{"altro_affine", "22", "altro affine", "altro affine", "altro affine"},
	{"altro", "", "altro", "altro", "altro"},
}

// relazioneAliases maps ways of naming a relazione, compared with
// lettersAndDigits, to its key. Every key also names itself.
var relazioneAliases = map[string]string{
	"marito": "coniuge", "moglie": "coniuge", "sposo": "coniuge", "sposa": "coniuge",
	"husband": "coniuge", "wife": "coniuge", "spouse": "coniuge",
	"unitacivilmente": "unito_civilmente", "unitoacivilmente": "unito_civilmente", "unionecivile": "unito_civilmente", "civilpartner": "unito_civilmente",
	"compagno": "convivente", "compagna": "convivente", "partner": "convivente", "conviventedifatto": "convivente",
	"figlia": "figlio", "figlioa": "figlio", "son": "figlio", "daughter": "figlio", "child": "figlio",
	"grandson": "nipote", "granddaughter": "nipote", "grandchild": "nipote",
	"nephew": "nipote_collaterale", "niece": "nipote_collaterale",
	"padre": "genitore", "madre": "genitore", "mamma": "genitore", "papà": "genitore", "papa": "genitore",
	"father": "genitore", "mother": "genitore", "parent": "genitore",
	"nonna": "nonno", "nonnoa": "nonno", "grandfather": "nonno", "grandmother": "nonno", "grandparent": "nonno",
	"sorella": "fratello", "fratellosorella": "fratello", "brother": "fratello", "sister": "fratello", "sibling": "fratello",
	"zia": "zio", "zioa": "zio", "uncle": "zio", "aunt": "zio",
	"cugina": "cugino", "cuginoa": "cugino", "cousin": "cugino",
	"genero": "genero_nuora", "nuora": "genero_nuora", "soninlaw": "genero_nuora", "daughterinlaw": "genero_nuora",
	"suocera": "suocero", "suoceroa": "suocero", "fatherinlaw": "suocero", "motherinlaw": "suocero",
	"cognata": "cognato", "cognatoa": "cognato", "brotherinlaw": "cognato", "sisterinlaw": "cognato",
	"other": "altro",
}

func init() {
	for _, r := range relazioni {
		relazioneAliases[lettersAndDigits(r.key)] = r.key
	}
}

// relazioneKeys lists the relazioni for messages and the schema.
func relazioneKeys() []string {
	keys := make([]string, len(relazioni))
	for i, r := range relazioni {
		keys[i] = r.key
	}
	return keys
}

// findRelazione returns the relazione stored as key.
func findRelazione(key string) (relazione, bool) {
	for _, r := range relazioni {
		if r.key == key {
			return r, true
		}
	}
	return relazione{}, false
}

// RelazioneANPR returns the ANPR Tabella 05 code for a stored relazione,
// or "" when it has none.
func RelazioneANPR(key string) string {
	r, _ := findRelazione(key)
	return r.anpr
}

// RelazioneLabel returns the word forms use for a relazione (moglie,
// figlia, padre), from the member's sex as stored in identity.sex.
func RelazioneLabel(key, sex string) string {
	r, ok := findRelazione(key)
	if !ok {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(sex)) {
	case "M":
		return r.male
	case "F":
		return r.female
	}
	return r.neutral
}

// rappresentanze are the ways the owner can act for an adult member (for a
// minor it follows from famiglia.genitori).
var rappresentanze = []string{"tutore", "amministratore_sostegno", "curatore", "delega"}

var rappresentanzaAliases = map[string]string{
	"amministratoredisostegno": "amministratore_sostegno", "amministratoresostegno": "amministratore_sostegno", "ads": "amministratore_sostegno",
	"tutela": "tutore", "curatela": "curatore", "delegato": "delega",
}

// PersonRefs splits a value naming people (famiglia.genitori,
// famiglia.intestatario_scheda, pratiche persona) into its references:
// "io" or member slugs.
func PersonRefs(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
}

// isPersonRefField reports the fields whose value names people.
func isPersonRefField(category, name string) bool {
	switch category + "." + name {
	case "famiglia.genitori", "famiglia.intestatario_scheda":
		return true
	}
	_, campo, ok := praticaField(category, name)
	return ok && campo == "persona"
}

// normalizePersonRefs checks a list of 1..max people and stores them as
// "io,giulia". self is the member whose field it is, who can't be named.
func normalizePersonRefs(value, self string, max int) (string, string) {
	refs := PersonRefs(value)
	if len(refs) == 0 || len(refs) > max {
		if max == 1 {
			return "", "must be io (you) or one member's slug"
		}
		return "", "must name 1 or 2 people: io (you) or members' slugs, e.g. io,giulia"
	}
	for i, ref := range refs {
		if ref != OwnerRef && !ValidMemberSlug(ref) {
			return "", "\"" + ref + "\" is not io or a member's slug"
		}
		if ref == self {
			return "", "a person can't be their own genitore"
		}
		if slices.Contains(refs[:i], ref) {
			return "", "\"" + ref + "\" is named twice"
		}
	}
	return strings.Join(refs, ","), ""
}

// normalizeFamiglia applies the famiglia rules. handled is false outside
// the famiglia category, so the generic rules still apply there.
func normalizeFamiglia(id, member, category, name, value string) (out string, handled bool, err error) {
	if category != "famiglia" {
		return value, false, nil
	}
	fail := func(msg string) (string, bool, error) {
		return "", true, &ValidationError{Field: id, Msg: msg}
	}
	switch {
	case member != "" && slices.Contains(famigliaOwnerFields, name):
		return fail("is about you, the vault's owner: store it as famiglia." + name + " without a person prefix")
	case member == "" && slices.Contains(famigliaMemberFields, name):
		return fail("describes a family member: store it as <member>:famiglia." + name + " (e.g. leo:famiglia." + name + ")")
	}

	switch name {
	case "relazione":
		if key, ok := relazioneAliases[lettersAndDigits(value)]; ok {
			return key, true, nil
		}
		return fail("must be one of " + strings.Join(relazioneKeys(), ", "))
	case "stato_famiglia":
		return yesNo(value, fail)
	case "a_carico_percentuale":
		v := strings.TrimSuffix(strings.ReplaceAll(strings.TrimSpace(value), " ", ""), "%")
		if v == "0" || v == "50" || v == "100" {
			return v, true, nil
		}
		return fail("must be 0, 50 or 100 (the share of the IRPEF dependant you claim)")
	case "genitori":
		v, msg := normalizePersonRefs(value, member, 2)
		if msg != "" {
			return fail(msg)
		}
		return v, true, nil
	case "intestatario_scheda":
		// Older vaults stored si/no here: si means you, no says nothing.
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "si", "sì", "yes":
			return OwnerRef, true, nil
		case "no":
			return fail("name the intestatario: io (you) or a member's slug")
		}
		v, msg := normalizePersonRefs(value, "", 1)
		if msg != "" {
			return fail(msg)
		}
		return v, true, nil
	case "rappresentanza":
		v := normalizeChoice(value)
		if a, ok := rappresentanzaAliases[lettersAndDigits(value)]; ok {
			v = a
		}
		if slices.Contains(rappresentanze, v) {
			return v, true, nil
		}
		return fail("must be one of " + strings.Join(rappresentanze, ", "))
	}
	return value, false, nil
}

// yesNo normalizes a si/no answer.
func yesNo(value string, fail func(string) (string, bool, error)) (string, bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "si", "sì", "s", "yes", "y", "true":
		return "si", true, nil
	case "no", "n", "false":
		return "no", true, nil
	}
	return fail("must be si or no")
}

// famigliaDescription documents the category in the schema.
func famigliaDescription() string {
	return "How your family members relate to you, the vault's owner (as ANPR records every relazione against the intestatario della scheda). " +
		"Each member is a person with their own data under a prefix, e.g. leo:identity.codice_fiscale, leo:cie.numero; " +
		"add one with 'pvault member add <slug> --relazione <relazione>'. " +
		"Member fields: relazione (" + strings.Join(relazioneKeys(), ", ") + "); " +
		"stato_famiglia (si/no: in your stato di famiglia); a_carico_percentuale (0, 50 or 100); " +
		"genitori (io and/or member slugs, e.g. io,giulia); rappresentanza (" + strings.Join(rappresentanze, ", ") + "). " +
		"Yours: intestatario_scheda (io or a member's slug)."
}
