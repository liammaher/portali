package vault

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// ExpiryInfo describes a document expiry date found in the vault.
type ExpiryInfo struct {
	FieldID   string `json:"field_id"`
	Document  string `json:"document"`         // category without the person, e.g. permesso_soggiorno, or "pratica <slug>"
	Person    string `json:"member,omitempty"` // whose it is: a person's slug, empty for the owner (JSON key from before the rename)
	ExpiresOn string `json:"expires_on"`       // YYYY-MM-DD
	DaysLeft  int    `json:"days_left"`        // negative once expired
	Expired   bool   `json:"expired"`
	// Kind is "" for a document or pratica deadline, ExpiryRetention or
	// ExpiryPraticheClosed (retention.go).
	Kind string `json:"kind,omitempty"`
}

// Expiring returns expiry dates (fields named *data_scadenza) that have
// passed or fall within the given window, soonest first. Only fields allow
// accepts are decrypted. Values that are not valid dates (e.g. written by an
// older version) are skipped, and so are the deadlines of closed pratiche.
// It also reports what may be due for deletion (retention.go): a person whose
// ruolo.data_fine plus the retention period, if one is set, falls in the
// window, and, in a business vault, a client's ID and procura scans once
// every pratica naming them is closed (scansAfterPratiche). Both report only
// what allow accepts.
func (v *Vault) Expiring(within time.Duration, allow FieldFilter, now time.Time) ([]ExpiryInfo, error) {
	years, err := v.RetentionYears()
	if err != nil {
		return nil, err
	}
	ctx, err := v.decryptFields(func(id, sensitivity string) bool {
		_, local := SplitPerson(id)
		parts := strings.SplitN(local, ".", 2)
		if len(parts) != 2 || (allow != nil && !allow(id, sensitivity)) {
			return false
		}
		_, campo, _ := praticaField(parts[0], parts[1])
		return IsExpiryField(parts[1]) || campo == "stato" || campo == "persona" ||
			(years > 0 && isRuoloDataFine(parts[0], parts[1]))
	})
	if err != nil {
		return nil, err
	}

	// Pratiche whose stato ends them, and whom each one is for, by slug.
	closed := map[string]bool{}
	persona := map[string]string{}
	for _, fields := range ctx.Categories {
		for _, f := range fields {
			slug, campo, _ := praticaField(f.Category, f.FieldName)
			switch {
			case campo == "stato" && isClosedStato(f.Value):
				closed[slug] = true
			case campo == "persona" && f.Value != OwnerRef:
				persona[slug] = f.Value
			}
		}
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	cutoff := today.Add(within)
	result := []ExpiryInfo{}
	add := func(id, document, person, kind string, on time.Time) {
		days := int(on.Sub(today).Hours() / 24)
		result = append(result, ExpiryInfo{
			FieldID:   id,
			Document:  document,
			Person:    person,
			ExpiresOn: on.Format(dateLayout),
			DaysLeft:  days,
			Expired:   days < 0,
			Kind:      kind,
		})
	}
	for _, fields := range ctx.Categories {
		for _, f := range fields {
			person, document := SplitPerson(f.Category)
			if person != "" && isRuoloDataFine(document, f.FieldName) {
				if end, ok := retentionEnd(f.Value, years); ok && !end.After(cutoff) {
					add(f.ID, document, person, ExpiryRetention, end)
				}
				continue
			}
			slug, _, isPratica := praticaField(f.Category, f.FieldName)
			if !IsExpiryField(f.FieldName) || (isPratica && closed[slug]) {
				continue
			}
			normalized, err := NormalizeDate(f.Value)
			if err != nil {
				continue
			}
			expires, _ := time.Parse(dateLayout, normalized)
			if expires.After(cutoff) {
				continue
			}
			if isPratica {
				document = "pratica " + slug // a pratica's deadline
				person = persona[slug]
			}
			add(f.ID, document, person, "", expires)
		}
	}
	if v.kind == KindImpresa {
		due, err := v.scansAfterPratiche(allow)
		if err != nil {
			return nil, err
		}
		for _, d := range due {
			person, document := SplitPerson(d)
			add(DocumentScopeID(d), document, person, ExpiryPraticheClosed, today)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ExpiresOn < result[j].ExpiresOn })
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*.data_scadenza,pratiche.*_stato,*:ruolo.data_fine", Action: "expiring"})
	return result, nil
}

// scansAfterPratiche returns the document categories (rossi:cie) of ID and
// procura scans, sorted, of each client (ruolo.tipo includes cliente) whom
// at least one pratica names in persona and every pratica naming them is
// closed. Only clients: the business's own pratiche (persona io) use its
// titolare's and soci's documents too, without naming them.
//
// It reads every pratica, not just those allow accepts, so a pratica the
// caller can't see still counts as open; but a person counts only if the
// caller can read the persona and stato of every pratica naming them and
// their ruolo.tipo, and only scans the caller can see are returned. A
// partial view never says scans can go.
func (v *Vault) scansAfterPratiche(allow FieldFilter) ([]string, error) {
	ctx, err := v.decryptFields(func(id, _ string) bool {
		person, local := SplitPerson(id)
		category, name, _ := strings.Cut(local, ".")
		if person != "" {
			return strings.EqualFold(category, "ruolo") && strings.EqualFold(name, "tipo")
		}
		_, campo, _ := praticaField(category, name)
		return campo == "persona" || campo == "stato"
	})
	if err != nil {
		return nil, err
	}
	visible := func(f FieldInfo) bool { return allow == nil || allow(f.ID, f.Sensitivity) }
	type pratica struct {
		persona          string
		closed, readable bool
	}
	pratiche := map[string]*pratica{}
	clients := map[string]bool{}
	for _, fields := range ctx.Categories {
		for _, f := range fields {
			if person, _ := SplitPerson(f.Category); person != "" {
				clients[person] = visible(f) && slices.Contains(strings.Split(f.Value, ","), "cliente")
				continue
			}
			slug, campo, _ := praticaField(f.Category, f.FieldName)
			p := pratiche[slug]
			if p == nil {
				p = &pratica{readable: true}
				pratiche[slug] = p
			}
			p.readable = p.readable && visible(f)
			if campo == "stato" {
				p.closed = isClosedStato(f.Value)
			} else {
				p.persona = f.Value
			}
		}
	}
	done := map[string]bool{} // client → every pratica naming them is closed, and readable
	for _, p := range pratiche {
		for _, person := range PersonRefs(p.persona) {
			ok := clients[person] && p.closed && p.readable
			if prev, seen := done[person]; seen {
				ok = ok && prev
			}
			done[person] = ok
		}
	}
	docs, err := v.db.ListDocuments()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, doc := range docs {
		person, base := SplitPerson(doc.Category)
		if person == "" || !done[person] || !isIDScanCategory(base) || seen[doc.Category] ||
			(allow != nil && !allow(DocumentScopeID(doc.Category), doc.Sensitivity)) {
			continue
		}
		seen[doc.Category] = true
		out = append(out, doc.Category)
	}
	sort.Strings(out)
	return out, nil
}
