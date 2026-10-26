package vault

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// The people in a vault besides its owner (see scope.go for their IDs):
// fields that name them, and removing one. The family view of them
// (famiglia, Household, the nucleo migration) is in household.go.

// ErrPersonNotFound means no person in the vault has that slug.
var ErrPersonNotFound = errors.New("person not found")

// checkPersonRefs refuses a value naming a person who isn't in the vault
// (famiglia.genitori, famiglia.intestatario_scheda, pratiche persona).
func (v *Vault) checkPersonRefs(id, value string) error {
	_, local := SplitPerson(id)
	parts := strings.SplitN(strings.ToLower(local), ".", 2)
	if len(parts) != 2 || !isPersonRefField(parts[0], parts[1]) {
		return nil
	}
	for _, ref := range PersonRefs(value) {
		if ref == OwnerRef {
			continue
		}
		ok, err := v.db.PersonExists(ref)
		if err != nil {
			return err
		}
		if !ok {
			flag := "--relazione"
			if v.kind == KindImpresa {
				flag = "--ruolo"
			}
			return &ValidationError{Field: id, Msg: "there is no person \"" + ref + "\" yet: add them first with 'pvault person add " + ref + " " + flag + " ...'"}
		}
	}
	return nil
}

// isPersonRefField reports the fields whose value names people: those the
// schema marks PersonRef, and a pratica's persona. category and name are
// lower-case.
func isPersonRefField(category, name string) bool {
	if f := GetSchemaField(category + "." + name); f != nil && f.PersonRef {
		return true
	}
	_, campo, ok := praticaField(category, name)
	return ok && campo == "persona"
}

// PersonInUseError lists the fields that still name a person being removed.
type PersonInUseError struct {
	Slug string
	Refs []string
}

func (e *PersonInUseError) Error() string {
	return fmt.Sprintf("%s is still named in %s: change or delete those first", e.Slug, strings.Join(e.Refs, ", "))
}

// RemovedPerson reports what RemovePerson deleted.
type RemovedPerson struct {
	Fields        int      `json:"fields"`
	Documents     int      `json:"documents"`
	RevokedTokens []string `json:"revoked_tokens"` // consumers of service tokens that named the person
	DroppedGrants int      `json:"dropped_grants"`
}

// RemovePerson deletes every field and document of a person, in one
// transaction, and revokes the service tokens and scoped grants whose scope
// names them, so a later person with the same slug can't inherit them. It
// refuses while another field names the person (PersonInUseError).
func (v *Vault) RemovePerson(slug string) (*RemovedPerson, error) {
	if !ValidPersonSlug(slug) {
		return nil, &ValidationError{Field: "person", Msg: errPersonSlug(slug).Error()}
	}
	// Lock order: backup.mu, then writeMu, then v.mu. Holding backup.mu
	// keeps a backup from snapshotting rows whose files are being removed.
	v.backup.mu.Lock()
	defer v.backup.mu.Unlock()
	v.writeMu.Lock()
	defer v.writeMu.Unlock()
	if err := v.checkUnlocked(); err != nil {
		return nil, err
	}

	fieldIDs, docIDs, err := v.db.PersonRows(slug)
	if err != nil {
		return nil, err
	}
	if len(fieldIDs) == 0 && len(docIDs) == 0 {
		return nil, ErrPersonNotFound
	}
	refs, err := v.personRefsTo(slug)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		return nil, &PersonInUseError{Slug: slug, Refs: refs}
	}
	if err := v.db.ApplyPersonChange(fieldIDs, docIDs, nil); err != nil {
		return nil, err
	}
	for _, id := range docIDs {
		if err := os.Remove(v.documentPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove document file: %w", err)
		}
	}

	out := &RemovedPerson{Fields: len(fieldIDs), Documents: len(docIDs), RevokedTokens: []string{}}
	tokens, err := v.db.ListTokensByUsage("service")
	if err != nil {
		return nil, err
	}
	for _, t := range tokens {
		if ScopeNamesPerson(t.Scope, slug) {
			if _, err := v.db.DeleteToken(t.TokenStr); err != nil {
				return nil, err
			}
			out.RevokedTokens = append(out.RevokedTokens, t.Consumer)
		}
	}
	v.mu.RLock()
	if v.session != nil {
		out.DroppedGrants = v.session.DropGrantsNaming(slug)
	}
	v.mu.RUnlock()

	// The audit action keeps its name from before the rename.
	v.db.LogAccess(store.AuditEntry{
		Consumer: "vault", Scope: slug + ":*", Action: "member_remove",
		Purpose: fmt.Sprintf("%d fields, %d documents, %d tokens revoked", out.Fields, out.Documents, len(out.RevokedTokens)),
	})
	v.markChanged()
	return out, nil
}

// personRefsTo returns the IDs of fields outside the person's own data whose
// value names slug. A closed pratica (completata, respinta, annullata) is
// history, not a reference: it doesn't keep a person from being removed.
func (v *Vault) personRefsTo(slug string) ([]string, error) {
	ctx, err := v.decryptFields(func(id, _ string) bool {
		person, local := SplitPerson(id)
		parts := strings.SplitN(strings.ToLower(local), ".", 2)
		if person == slug || len(parts) != 2 {
			return false
		}
		_, campo, _ := praticaField(parts[0], parts[1])
		return isPersonRefField(parts[0], parts[1]) || campo == "stato"
	})
	if err != nil {
		return nil, err
	}
	closed := map[string]bool{}
	for _, f := range ctx.Categories["pratiche"] {
		if pslug, campo, _ := praticaField(f.Category, f.FieldName); campo == "stato" && isClosedStato(f.Value) {
			closed[pslug] = true
		}
	}
	var refs []string
	for _, fs := range ctx.Categories {
		for _, f := range fs {
			pslug, campo, isPratica := praticaField(f.Category, f.FieldName)
			if campo == "stato" || (isPratica && closed[pslug]) {
				continue
			}
			if slices.Contains(PersonRefs(f.Value), slug) {
				refs = append(refs, f.ID)
			}
		}
	}
	sort.Strings(refs)
	return refs, nil
}
