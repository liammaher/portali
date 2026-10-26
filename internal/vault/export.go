package vault

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// Exporting one person's data, for their right to data portability (GDPR
// art. 20): every field and document stored about them, and the fields of
// every pratica that names them (a client's CILA, a child's CIE), in one call
// the audit log records. The documents attached to pratiche are not theirs
// alone to tell: they sit in one 'pratiche' category, not per pratica, so they
// are not exported. Only the user's session may (the API enforces it).

// PersonExport is everything stored about one person.
type PersonExport struct {
	Person    string             `json:"person"`
	Exported  time.Time          `json:"exported"`
	Fields    []FieldInfo        `json:"fields"`
	Documents []ExportedDocument `json:"documents"`
	// Omitted counts the personal-tier fields and documents left out.
	Omitted int `json:"omitted,omitempty"`
	// Missing lists documents whose encrypted file is gone (only the
	// metadata is left), so one lost file doesn't block the export.
	Missing []DocumentInfo `json:"missing,omitempty"`
}

// ExportedDocument is a document with its decrypted content.
type ExportedDocument struct {
	DocumentInfo
	Data []byte `json:"data"` // base64 in JSON
}

// ExportPerson returns a person's fields and documents, sorted by ID and by
// category and name. Personal-tier items (SPID/CieID logins, PINs) are left
// out unless includePersonal, and counted in Omitted. ErrPersonNotFound if
// the vault has nothing about slug.
func (v *Vault) ExportPerson(slug string, includePersonal bool) (*PersonExport, error) {
	if !ValidPersonSlug(slug) {
		return nil, &ValidationError{Field: "person", Msg: errPersonSlug(slug).Error()}
	}
	// Held for reading, so RemovePerson (which holds it to write) can't
	// delete rows and files between the listing and the reads.
	v.writeMu.RLock()
	defer v.writeMu.RUnlock()
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return nil, err
	}
	defer clear(vaultKey)
	if exists, err := v.db.PersonExists(slug); err != nil || !exists {
		if err == nil {
			err = ErrPersonNotFound
		}
		return nil, err
	}

	out := &PersonExport{Person: slug, Exported: time.Now().UTC(), Fields: []FieldInfo{}, Documents: []ExportedDocument{}}
	keep := func(sensitivity string) bool {
		if sensitivity == TierPersonal && !includePersonal {
			out.Omitted++
			return false
		}
		return true
	}
	// The pratiche that name them: their persona is read before the fields
	// are chosen, so none is missed whatever the order.
	named := map[string]bool{}
	personas, err := v.decryptFields(func(id, _ string) bool {
		_, campo, ok := praticaOfID(id)
		return ok && campo == "persona"
	})
	if err != nil {
		return nil, err
	}
	for pslug, p := range praticheIn(personas, nil) {
		if slices.Contains(p.persone, slug) {
			named[pslug] = true
		}
	}
	ctx, err := v.decryptFields(func(id, sensitivity string) bool {
		person, _ := SplitPerson(id)
		pslug, _, isPratica := praticaOfID(id)
		return (person == slug || (isPratica && named[pslug])) && keep(sensitivity)
	})
	if err != nil {
		return nil, err
	}
	for _, fs := range ctx.Categories {
		out.Fields = append(out.Fields, fs...)
	}
	sort.Slice(out.Fields, func(i, j int) bool { return out.Fields[i].ID < out.Fields[j].ID })

	docs, err := v.db.ListDocuments()
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if person, _ := SplitPerson(doc.Category); person != slug || !keep(doc.Sensitivity) {
			continue
		}
		info, err := v.decryptDocumentInfo(vaultKey, doc)
		if err != nil {
			return nil, err
		}
		data, err := v.decryptDocumentContent(vaultKey, doc)
		if errors.Is(err, os.ErrNotExist) {
			out.Missing = append(out.Missing, *info)
			continue
		}
		if err != nil {
			return nil, err
		}
		out.Documents = append(out.Documents, ExportedDocument{DocumentInfo: *info, Data: data})
	}
	sort.Slice(out.Documents, func(i, j int) bool {
		a, b := out.Documents[i], out.Documents[j]
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.Name < b.Name
	})

	// The audit action follows member_remove's naming.
	v.db.LogAccess(store.AuditEntry{
		Consumer: "vault", Scope: slug + ":*", Action: "member_export",
		Purpose: fmt.Sprintf("%d fields, %d documents, %d personal left out (include_personal=%t), %d files missing", len(out.Fields), len(out.Documents), out.Omitted, includePersonal, len(out.Missing)),
	})
	return out, nil
}
