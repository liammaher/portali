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
// window, and, in a business vault where the user set a period for scans
// (ScanDays), a client's ID and procura scans once that period after every
// pratica naming them closed ends (scansAfterPratiche). Both report only what
// allow accepts.
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

	// Which pratiche are closed, and whom each one is for (from what allow
	// accepts: a stato it can't see leaves its pratica's deadline showing).
	pratiche := praticheIn(ctx, nil)

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
			if !IsExpiryField(f.FieldName) || (isPratica && pratiche[slug] != nil && pratiche[slug].closed()) {
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
				if p := pratiche[slug]; p != nil {
					persone := slices.DeleteFunc(slices.Clone(p.persone), func(s string) bool { return s == OwnerRef })
					slices.Sort(persone)
					person = strings.Join(slices.Compact(persone), ",")
				}
			}
			add(f.ID, document, person, "", expires)
		}
	}
	if v.kind == KindImpresa {
		days, set, err := v.ScanDays()
		if err != nil {
			return nil, err
		}
		if set {
			scans, err := v.scansAfterPratiche(allow, today)
			if err != nil {
				return nil, err
			}
			for _, s := range scans {
				// The day it closed where now is, like today.
				at := s.closedAt.In(now.Location())
				closedOn := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
				if due := closedOn.AddDate(0, 0, days); !due.After(cutoff) {
					person, document := SplitPerson(s.category)
					add(DocumentScopeID(s.category), document, person, ExpiryPraticheClosed, due)
				}
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ExpiresOn < result[j].ExpiresOn })
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*.data_scadenza,pratiche.*_stato,*:ruolo.data_fine", Action: "expiring"})
	return result, nil
}

// idScans is a client's document category whose scans scansAfterPratiche
// reports, and when the last pratica naming the client closed.
type idScans struct {
	category string
	closedAt time.Time
}

// scansAfterPratiche returns the document categories (rossi:cie) of ID and
// procura scans, sorted, of each client (ruolo.tipo includes cliente) whom
// at least one pratica names in persona and every pratica naming them is
// closed. Only clients: the business's own pratiche (persona io) use its
// titolare's and soci's documents too, without naming them. A category is
// returned only if every scan in it was there before the last of those
// pratiche closed (ClosedAt; times are to the second, so a scan from the same
// second counts as after), so a scan handed over for a new job, or for a later
// person given a removed person's slug, is never due; and a procura category
// is kept while a procura.data_scadenza is still ahead.
//
// It reads every pratica, not just those allow accepts, so a pratica the
// caller can't see still counts as open; but a person counts only if the
// caller can read the persona and stato of every pratica naming them, their
// ruolo.tipo and procura.data_scadenza, and every scan in the category. A
// partial view never says scans can go.
func (v *Vault) scansAfterPratiche(allow FieldFilter, today time.Time) ([]idScans, error) {
	ctx, err := v.decryptFields(func(id, _ string) bool {
		person, local := SplitPerson(id)
		category, name, _ := strings.Cut(local, ".")
		if person != "" {
			return isRuoloTipo(category, name) || isProcuraScadenza(category, name)
		}
		_, campo, _ := praticaField(category, name)
		return campo == "persona" || campo == "stato"
	})
	if err != nil {
		return nil, err
	}
	visible := func(f FieldInfo) bool { return allow == nil || allow(f.ID, f.Sensitivity) }
	client, blocked := map[string]bool{}, map[string]bool{} // blocked: a ruolo.tipo or procura date the caller can't read
	procuraInForce := map[string]bool{}
	for _, fields := range ctx.Categories {
		for _, f := range fields {
			person, local := SplitPerson(f.Category)
			switch {
			case person == "":
			case !visible(f):
				blocked[person] = true
			case local == "ruolo":
				client[person] = client[person] || slices.Contains(strings.Split(f.Value, ","), "cliente")
			default: // a procura's data_scadenza
				if d, err := NormalizeDate(f.Value); err != nil || d >= today.Format(dateLayout) {
					procuraInForce[person] = true
				}
			}
		}
	}
	done := map[string]bool{}            // client → every pratica naming them is closed, and readable
	lastClosed := map[string]time.Time{} // client → when the last of those closed
	for _, p := range praticheIn(ctx, allow) {
		closed := p.closed() && !p.hidden
		for _, person := range p.persone {
			ok := client[person] && !blocked[person] && closed
			if prev, seen := done[person]; seen {
				ok = ok && prev
			}
			done[person] = ok
			if p.closedAt.After(lastClosed[person]) {
				lastClosed[person] = p.closedAt
			}
		}
	}
	docs, err := v.db.ListDocuments()
	if err != nil {
		return nil, err
	}
	due := map[string]bool{} // category → every scan in it may go
	for _, doc := range docs {
		person, base := SplitPerson(doc.Category)
		if person == "" || !done[person] || !isIDScanCategory(base) {
			continue
		}
		ok := (allow == nil || allow(DocumentScopeID(doc.Category), doc.Sensitivity)) &&
			doc.CreatedAt.Before(lastClosed[person]) && // same second: maybe after, so kept
			!(procuraInForce[person] && schemaCategoryOf(base).Name == "procura")
		if prev, seen := due[doc.Category]; seen {
			ok = ok && prev
		}
		due[doc.Category] = ok
	}
	var out []idScans
	for category, ok := range due {
		if ok {
			person, _ := SplitPerson(category)
			out = append(out, idScans{category, lastClosed[person]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].category < out[j].category })
	return out, nil
}

// isRuoloTipo reports a person's ruolo.tipo (category without the person).
func isRuoloTipo(category, name string) bool {
	return category == "ruolo" && name == "tipo"
}

// isProcuraScadenza reports a procura's data_scadenza, numbered copies too.
func isProcuraScadenza(category, name string) bool {
	c := schemaCategoryOf(category)
	return c != nil && c.Name == "procura" && name == "data_scadenza"
}
