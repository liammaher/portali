package vault

import (
	"slices"
	"strconv"
	"strings"
)

// The ruolo category records what each person in a business vault is to the
// business: its titolare, a socio, an employee, a client. It is the business
// counterpart of famiglia (personal vaults only); like famiglia it holds
// facts, and which roles a procedure needs is up to its playbook. Who signs
// as legale rappresentante is the person whose ruolo.tipo says so, so the
// business has no field naming them.
//
// Person: bianchi:ruolo.tipo, data_inizio, data_fine, quota_percentuale, poteri

// ruoli are the accepted roles, in the order the schema lists them.
var ruoli = []string{
	"titolare", "legale_rappresentante", "socio", "amministratore", "procuratore",
	"preposto", "responsabile_tecnico", "dipendente", "collaboratore",
	"cliente", "fornitore", "impresa_esecutrice", "altro",
}

// ruoloAliases maps ways of naming a role, compared with lettersAndDigits,
// to its key. Every key also names itself.
var ruoloAliases = map[string]string{
	"titolaredittaindividuale": "titolare", "owner": "titolare",
	"rappresentantelegale": "legale_rappresentante", "legalerapp": "legale_rappresentante", "legalrepresentative": "legale_rappresentante",
	"socia": "socio", "partner": "socio", "shareholder": "socio",
	"amministratrice": "amministratore", "amministratoreunico": "amministratore", "consigliere": "amministratore",
	"consigliera": "amministratore", "director": "amministratore", "procuratrice": "procuratore",
	"responsabiletecnica": "responsabile_tecnico", "dipendenti": "dipendente", "employee": "dipendente",
	"lavoratore": "dipendente", "lavoratrice": "dipendente",
	"collaboratrice": "collaboratore", "consulente": "collaboratore", "freelance": "collaboratore",
	"committente": "cliente", "client": "cliente", "customer": "cliente",
	"supplier": "fornitore", "other": "altro",
	"impresa": "impresa_esecutrice", "dittaesecutrice": "impresa_esecutrice",
}

func init() {
	for _, r := range ruoli {
		ruoloAliases[lettersAndDigits(r)] = r
	}
}

// ruoloKeys lists the roles for messages and the schema.
func ruoloKeys() []string { return slices.Clone(ruoli) }

// normalizeRuoli checks a list of one or more roles and stores them as
// "socio,amministratore". Items are split on commas and semicolons only, so
// "legale rappresentante" is one role.
func normalizeRuoli(value string) (string, string) {
	var out []string
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' }) {
		if strings.TrimSpace(item) == "" {
			continue
		}
		key, ok := ruoloAliases[lettersAndDigits(item)]
		if !ok {
			return "", "\"" + strings.TrimSpace(item) + "\" is not a ruolo: use " + strings.Join(ruoli, ", ")
		}
		if !slices.Contains(out, key) {
			out = append(out, key)
		}
	}
	if len(out) == 0 {
		return "", "name at least one ruolo: " + strings.Join(ruoli, ", ")
	}
	return strings.Join(out, ","), ""
}

// normalizeRuolo applies the ruolo rules. handled is false outside the ruolo
// category, so the generic rules still apply there (data_inizio and
// data_fine are dates by the generic rule).
func normalizeRuolo(id, category, name, value string) (out string, handled bool, err error) {
	if category != "ruolo" {
		return value, false, nil
	}
	fail := func(msg string) (string, bool, error) {
		return "", true, &ValidationError{Field: id, Msg: msg}
	}
	switch name {
	case "tipo":
		v, msg := normalizeRuoli(value)
		if msg != "" {
			return fail(msg)
		}
		return v, true, nil
	case "quota_percentuale":
		v, ok := normalizeDecimal(strings.TrimSuffix(strings.TrimSpace(value), "%"))
		if !ok || !percentAtMost100(v) {
			return fail("must be a share above 0 and up to 100 (e.g. 50 or 33.33)")
		}
		return v, true, nil
	}
	return value, false, nil
}

// percentAtMost100 reports a normalized decimal no greater than 100.
func percentAtMost100(v string) bool {
	f, err := strconv.ParseFloat(v, 64)
	return err == nil && f <= 100
}

// ruoloDescription documents the category in the schema.
func ruoloDescription() string {
	return "What each person in a business vault is to the business (business vaults only; personal vaults use famiglia). " +
		"Each person has their own data under a prefix, e.g. bianchi:identity.codice_fiscale, bianchi:albo.numero; " +
		"add one with 'pvault person add <slug> --ruolo <ruolo>'. " +
		"Fields: tipo (one or more of " + strings.Join(ruoli, ", ") + "); data_inizio, data_fine; " +
		"quota_percentuale (a socio's share, up to 100); poteri (an amministratore's or procuratore's powers). " +
		"A business vault never holds other people's SPID/CieID accounts or login secrets."
}
