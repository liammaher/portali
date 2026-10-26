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
// (famiglia.genitori, famiglia.intestatario_scheda, pratiche persona). With
// a filter (a service token's), a person counts only if the caller can see
// one of their rows, so a hidden person is refused like a missing one.
func (v *Vault) checkPersonRefs(id, value string, allow FieldFilter) error {
	_, local := SplitPerson(id)
	parts := strings.SplitN(strings.ToLower(local), ".", 2)
	if len(parts) != 2 || !isPersonRefField(parts[0], parts[1]) {
		return nil
	}
	for _, ref := range PersonRefs(value) {
		if ref == OwnerRef {
			continue
		}
		ok, err := v.personVisible(ref, allow)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if allow != nil {
			return &ValidationError{Field: id, Msg: "there is no person \"" + ref + "\" that you can see"}
		}
		flag := "--relazione"
		if v.kind == KindImpresa {
			flag = "--ruolo"
		}
		return &ValidationError{Field: id, Msg: "there is no person \"" + ref + "\" yet: add them first with 'pvault person add " + ref + " " + flag + " ...'"}
	}
	return nil
}

// personVisible reports whether slug has a field or document allow accepts
// (any, if allow is nil).
func (v *Vault) personVisible(slug string, allow FieldFilter) (bool, error) {
	if allow == nil {
		return v.db.PersonExists(slug)
	}
	fields, docs, err := v.db.PersonRowTiers(slug)
	if err != nil {
		return false, err
	}
	for _, f := range fields {
		if allow(f.ID, f.Sensitivity) {
			return true, nil
		}
	}
	for _, d := range docs {
		if allow(DocumentScopeID(d.Category), d.Sensitivity) {
			return true, nil
		}
	}
	return false, nil
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
	FilesLeft     []string `json:"files_left,omitempty"` // encrypted document files that could not be deleted
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
	// Tokens go in the same transaction as the rows: a removal that half
	// failed would leave them valid for a later person with the same slug,
	// and could not be retried once the rows were gone.
	tokens, err := v.db.ListTokensByUsage("service")
	if err != nil {
		return nil, err
	}
	out := &RemovedPerson{Fields: len(fieldIDs), Documents: len(docIDs), RevokedTokens: []string{}}
	var revoke []string
	for _, t := range tokens {
		if ScopeNamesPerson(t.Scope, slug) {
			revoke = append(revoke, t.TokenStr)
			out.RevokedTokens = append(out.RevokedTokens, t.Consumer)
		}
	}
	if err := v.db.ApplyPersonChange(fieldIDs, docIDs, revoke, nil); err != nil {
		return nil, err
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

	// Files last: the rows are gone, so a file left behind is an unreadable
	// encrypted orphan, but the user should know it is still on disk.
	for _, id := range docIDs {
		if err := os.Remove(v.documentPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			out.FilesLeft = append(out.FilesLeft, v.documentPath(id))
		}
	}
	return out, nil
}

// checkNotLastPersonRow refuses to delete a person's last field or document
// on its own: the person would be gone without RemovePerson's checks, leaving
// fields that name them and tokens and grants that a later person with the
// same slug would inherit. id is the field or document ID and category its
// category. Only rows allow accepts count as others, so a token's answer
// never depends on rows it can't see (it may be refused although hidden rows
// remain). The caller holds writeMu exclusively (lockWrite), so two deletes
// can't each leave the other's row as the last one.
func (v *Vault) checkNotLastPersonRow(category, id string, allow FieldFilter) error {
	person, _ := SplitPerson(category)
	if person == "" {
		return nil
	}
	fields, docs, err := v.db.PersonRowTiers(person)
	if err != nil {
		return err
	}
	visible := func(id, tier string) bool { return allow == nil || allow(id, tier) }
	found, others := false, 0
	for _, f := range fields {
		switch {
		case f.ID == id:
			found = true
		case visible(f.ID, f.Sensitivity):
			others++
		}
	}
	for _, d := range docs {
		switch {
		case d.ID == id:
			found = true
		case visible(DocumentScopeID(d.Category), d.Sensitivity):
			others++
		}
	}
	if found && others == 0 {
		return &ValidationError{Field: person, Msg: fmt.Sprintf(
			"this would leave %s with no fields or documents you can see: remove %s with 'pvault person remove %s' instead, which also revokes the tokens that name them",
			person, person, person)}
	}
	return nil
}

// lockWrite takes writeMu for a write to category and returns its unlock:
// exclusively for a person's rows (see checkNotLastPersonRow), else shared.
func (v *Vault) lockWrite(category string) (unlock func()) {
	if person, _ := SplitPerson(category); person != "" {
		v.writeMu.Lock()
		return v.writeMu.Unlock
	}
	v.writeMu.RLock()
	return v.writeMu.RUnlock
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
	// Closed only if every stato says so (a pratica stored under two
	// spellings of pratiche, from before IDs had to be lower-case).
	closed := map[string]bool{}
	for _, fs := range ctx.Categories {
		for _, f := range fs {
			if pslug, campo, _ := praticaField(f.Category, f.FieldName); campo == "stato" {
				prev, seen := closed[pslug]
				closed[pslug] = isClosedStato(f.Value) && (prev || !seen)
			}
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
