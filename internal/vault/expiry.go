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
	// Kind is "" for a document or pratica deadline, ExpiryRetention,
	// ExpiryPraticheClosed or ExpiryFilesClosed (retention.go).
	Kind string `json:"kind,omitempty"`
	// FileID and File are, for ExpiryFilesClosed, the indexed file and its
	// name. The vault doesn't hold such a file and can't delete it.
	FileID string `json:"file_id,omitempty"`
	File   string `json:"file,omitempty"`
}

// Expiring returns expiry dates (fields named *data_scadenza) that have
// passed or fall within the given window, soonest first. Only fields allow
// accepts are decrypted. Values that are not valid dates (e.g. written by an
// older version) are skipped, and so are the deadlines of closed pratiche.
// It also reports what may be due for deletion (retention.go): a person whose
// ruolo.data_fine plus the retention period, if one is set, falls in the
// window, and, in a business vault where the user set a period for scans
// (ScanDays), a client's ID and procura scans once that period after every
// pratica naming them closed ends (closedClientScans). Both report only what
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
			scans, err := v.closedClientScans(allow, today)
			if err != nil {
				return nil, err
			}
			dueOn := func(closedAt time.Time) time.Time {
				// The day it closed where now is, like today.
				at := closedAt.In(now.Location())
				return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, days)
			}
			for _, s := range scans.docs {
				if due := dueOn(s.closedAt); !due.After(cutoff) {
					person, document := SplitPerson(s.category)
					add(DocumentScopeID(s.category), document, person, ExpiryPraticheClosed, due)
				}
			}
			for _, f := range scans.files {
				if due := dueOn(f.closedAt); !due.After(cutoff) {
					add(FilesScopeID(f.holder), f.role, f.holder, ExpiryFilesClosed, due)
					result[len(result)-1].FileID, result[len(result)-1].File = f.fileID, f.name
				}
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ExpiresOn < result[j].ExpiresOn })
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: "*.data_scadenza,pratiche.*_stato,*:ruolo.data_fine", Action: "expiring"})
	return result, nil
}

// idScans is a client's document category whose scans closedClientScans
// reports, and when the last pratica naming the client closed.
type idScans struct {
	category string
	closedAt time.Time
}

// closedScans is what a business vault's closed pratiche let go of: the
// vault's own scans, and files in its index that are linked to a client as a
// copy of an ID or procura (the vault can only report those).
type closedScans struct {
	docs  []idScans
	files []fileDue
}

// closedClientScans returns the document categories (rossi:cie) of ID and
// procura scans, sorted, of each client (ruolo.tipo includes cliente) whom
// at least one pratica names in persona and every pratica naming them is
// closed. Only clients: the business's own pratiche (persona io) use its
// titolare's and soci's documents too, without naming them. A category is
// returned only if every scan in it was there before the last of those
// pratiche closed (ClosedAt; times are to the second, so a scan from the same
// second counts as after), so a scan handed over for a new job, or for a later
// person given a removed person's slug, is never due; and a procura category
// is kept while its data_scadenza is still ahead, or missing: a mandate with
// no end date (valid until revoked) is still in force.
//
// It reads every pratica, not just those allow accepts, so a pratica the
// caller can't see still counts as open; but a person counts only if the
// caller can read the persona and stato of every pratica naming them, their
// ruolo.tipo and procura.data_scadenza, and every scan in the category. A
// partial view never says scans can go. Files in the index linked to such a
// client as an ID or procura copy come with them (filesDue).
func (v *Vault) closedClientScans(allow FieldFilter, today time.Time) (*closedScans, error) {
	c, err := v.clientClosure(allow, today)
	if err != nil {
		return nil, err
	}
	docs, err := v.scanCategoriesDue(allow, c)
	if err != nil {
		return nil, err
	}
	files, err := v.filesDue(allow, c)
	if err != nil {
		return nil, err
	}
	return &closedScans{docs, files}, nil
}

// clientClosure is which clients have every pratica naming them closed (and
// readable by the caller), when the last of those closed, and what the
// caller can tell of each client's procura.
type clientClosure struct {
	done           map[string]bool      // client → every pratica naming them is closed, and readable
	lastClosed     map[string]time.Time // client → when the last of those closed
	procuraDated   map[string]bool      // by category (rossi:procura2)
	procuraInForce map[string]bool
}

func (v *Vault) clientClosure(allow FieldFilter, today time.Time) (*clientClosure, error) {
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
	client, blocked := map[string]bool{}, map[string]bool{}              // blocked: a ruolo.tipo or procura date the caller can't read
	procuraDated, procuraInForce := map[string]bool{}, map[string]bool{} // by category (rossi:procura2)
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
				procuraDated[f.Category] = true
				if d, err := NormalizeDate(f.Value); err != nil || d >= today.Format(dateLayout) {
					procuraInForce[f.Category] = true
				}
			}
		}
	}
	done := map[string]bool{}
	lastClosed := map[string]time.Time{}
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
	return &clientClosure{done, lastClosed, procuraDated, procuraInForce}, nil
}

// scanCategoriesDue returns the document categories whose scans may go.
func (v *Vault) scanCategoriesDue(allow FieldFilter, c *clientClosure) ([]idScans, error) {
	done, lastClosed, procuraDated, procuraInForce := c.done, c.lastClosed, c.procuraDated, c.procuraInForce
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
			!(schemaCategoryOf(base).Name == "procura" && (procuraInForce[doc.Category] || !procuraDated[doc.Category]))
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

// fileDue is an indexed file that may go: linked to a client as a copy of an
// ID or procura, every pratica naming the client closed.
type fileDue struct {
	holder   string
	role     string // the ID category the link says it is: cie, procura...
	fileID   string
	name     string
	closedAt time.Time
}

// linkRoleCategory reads a link's role as an ID category (cie, passaporto,
// "permesso soggiorno" and "permesso-soggiorno" both permesso_soggiorno), "" if
// it is none of those.
func linkRoleCategory(role string) string {
	base := strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(role)))
	if isIDScanCategory(base) {
		return base
	}
	return ""
}

// procuraMayGo reports that every procura of a client is over: there is at
// least one, each has a data_scadenza, and none is still ahead. A procura with
// no date is in force, as for the vault's own scans, so one undated copy
// among several keeps them all.
func (v *Vault) procuraMayGo(c *clientClosure, person string) (bool, error) {
	cats := map[string]bool{}
	fieldIDs, _, err := v.db.PersonRows(person)
	if err != nil {
		return false, err
	}
	for _, id := range fieldIDs {
		_, local := SplitPerson(id)
		cat, _, _ := strings.Cut(local, ".")
		if cs := schemaCategoryOf(cat); cs != nil && cs.Name == "procura" {
			cats[person+":"+cat] = true
		}
	}
	docs, err := v.db.ListDocuments()
	if err != nil {
		return false, err
	}
	for _, d := range docs {
		p, base := SplitPerson(d.Category)
		if cs := schemaCategoryOf(base); p == person && cs != nil && cs.Name == "procura" {
			cats[d.Category] = true
		}
	}
	if len(cats) == 0 {
		return false, nil
	}
	for cat := range cats {
		if !c.procuraDated[cat] || c.procuraInForce[cat] {
			return false, nil
		}
	}
	return true, nil
}

// filesDue returns the files in the index that the pratiche's closing lets
// go. A file qualifies only if every link it has is to a client who is done
// and names an ID or procura category as its role (so a file also linked to
// an open pratica, to the owner, to a client still being served, or as a
// contract, stays), the caller sees all of its links, no procura of those
// clients is still running, and it was last modified before the EARLIEST of
// those clients' closings (a file touched after one of them closed may be
// there for a new job; one with no time is kept). It is due when the latest
// of those closings plus the period has come. A file gone from its source
// isn't listed. They are references: the vault can't delete them.
func (v *Vault) filesDue(allow FieldFilter, c *clientClosure) ([]fileDue, error) {
	links, err := v.db.ListFileLinks("")
	if err != nil {
		return nil, err
	}
	byFile := map[string][]store.FileLink{}
	for _, l := range links {
		byFile[l.FileID] = append(byFile[l.FileID], l)
	}
	files, err := v.db.ListFiles("")
	if err != nil {
		return nil, err
	}
	var out []fileDue
	err = v.withFileKey(func(key []byte) error {
		procura := map[string]bool{} // holder → every procura of theirs is over
		for _, f := range files {
			ls := byFile[f.ID]
			if len(ls) == 0 || !f.GoneAt.IsZero() {
				continue
			}
			var latest, earliest time.Time
			var idHolders []string
			roleOf := map[string]string{}
			ok := true
			for _, l := range ls {
				if l.Holder == "" || !c.done[l.Holder] {
					ok = false
					break
				}
				if _, vis := visibleFileLinks(allow, f, []store.FileLink{l}); !vis {
					ok = false
					break
				}
				if closed := c.lastClosed[l.Holder]; closed.After(latest) {
					latest = closed
				}
				if closed := c.lastClosed[l.Holder]; earliest.IsZero() || closed.Before(earliest) {
					earliest = closed
				}
				info, err := openFileLink(key, l)
				if err != nil {
					return err
				}
				role := linkRoleCategory(info.Role)
				if role == "" {
					ok = false // linked for another purpose: still needed
					break
				}
				if cs := schemaCategoryOf(role); cs != nil && cs.Name == "procura" {
					over, seen := procura[l.Holder]
					if !seen {
						if over, err = v.procuraMayGo(c, l.Holder); err != nil {
							return err
						}
						procura[l.Holder] = over
					}
					if !over {
						ok = false
						break
					}
				}
				if _, dup := roleOf[l.Holder]; !dup {
					idHolders = append(idHolders, l.Holder)
					roleOf[l.Holder] = role
				}
			}
			if !ok || len(idHolders) == 0 {
				continue
			}
			var meta FileMeta
			if err := openFileBlob(key, "file", f.ID, f.Blob, &meta); err != nil {
				return err
			}
			if at, err := time.Parse(time.RFC3339, meta.Mtime); err != nil || !at.Before(earliest) {
				continue
			}
			sort.Strings(idHolders)
			out = append(out, fileDue{idHolders[0], roleOf[idHolders[0]], f.ID, meta.Name, latest})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].holder != out[j].holder {
			return out[i].holder < out[j].holder
		}
		return out[i].fileID < out[j].fileID
	})
	return out, err
}
