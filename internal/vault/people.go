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
	if !isPersonRefID(local) {
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

// checkPersonExists refuses a write by a service token (allow set) under a
// person the vault doesn't have. A new person starts with the session or a
// grant the user approved ('pvault person add'), so an agent can't claim a
// slug first or skip the --ruolo a business vault asks for. id is a field ID
// or a document category.
func (v *Vault) checkPersonExists(id string, allow FieldFilter) error {
	person, _ := SplitPerson(id)
	if allow == nil || person == "" {
		return nil
	}
	exists, err := v.db.PersonExists(person)
	if err != nil {
		return err
	}
	if !exists {
		return &ValidationError{Field: id, Msg: "there is no person \"" + person + "\" in the vault: only the user adds people"}
	}
	return nil
}

// checkPraticaPersona refuses to open a pratica whose persona names someone
// no longer in the vault: RemovePerson lets a closed pratica keep naming
// them (it is history), but an open one must name people who exist. It reads
// persona whatever the caller may read; a service token (token) is told only
// that it names someone gone, not who.
func (v *Vault) checkPraticaPersona(vaultKey []byte, slug string, token bool) error {
	id := "pratiche." + slug + "_persona"
	f, err := v.db.GetField(id)
	if err != nil || f == nil {
		return err
	}
	persone, err := decryptValue(vaultKey, v.salt, "pratiche", f.Value)
	if err != nil {
		return err
	}
	if err := v.checkPersonRefs(id, persone, nil); err != nil {
		var verr *ValidationError
		if !errors.As(err, &verr) {
			return err
		}
		// Not "add them first": the slug stays a removed person's while a
		// pratica names it (checkNewPersonSlug).
		msg := "persona names someone who is no longer in the vault (" + persone + "): set it to io or someone in the vault first"
		if token {
			msg = "persona names someone who is no longer in the vault: ask the user to fix it first"
		}
		return &ValidationError{Field: "pratiche." + slug + "_stato", Msg: msg}
	}
	return nil
}

// checkNewPersonSlug refuses to start a person (id's prefix, not in the
// vault yet) whose slug a pratica still names. A closed pratica may keep
// naming a removed person (RemovePerson); a new person with that slug would
// inherit it: in expiring, in their export, and by reopening it.
func (v *Vault) checkNewPersonSlug(id string) error {
	person, _ := SplitPerson(id)
	if person == "" {
		return nil
	}
	exists, err := v.db.PersonExists(person)
	if err != nil || exists {
		return err
	}
	named, err := v.praticheNaming()
	if err != nil {
		return err
	}
	if refs := named[person]; len(refs) > 0 {
		return &ValidationError{Field: id, Msg: fmt.Sprintf("%s still names %q, a person who was removed: give the new person another slug, or delete those pratiche first",
			strings.Join(refs, ", "), person)}
	}
	return nil
}

// praticheNaming maps each person a pratica's persona names, open or closed,
// to those persona fields.
func (v *Vault) praticheNaming() (map[string][]string, error) {
	ctx, err := v.decryptFields(func(id, _ string) bool {
		person, local := SplitPerson(id)
		category, name, ok := strings.Cut(canonicalLocalID(local), ".")
		_, campo, isPratica := praticaField(category, name)
		return person == "" && ok && isPratica && campo == "persona"
	})
	if err != nil {
		return nil, err
	}
	named := map[string][]string{}
	for _, fs := range ctx.Categories {
		for _, f := range fs {
			for _, ref := range PersonRefs(f.Value) {
				if ref != OwnerRef {
					named[ref] = append(named[ref], f.ID)
				}
			}
		}
	}
	for _, ids := range named {
		sort.Strings(ids)
	}
	return named, nil
}

// visibleRefs drops from a value naming people the ones allow can't see a row
// of, so a token's view of the household never shows a slug it could not name.
func (v *Vault) visibleRefs(value string, allow FieldFilter) (string, error) {
	var kept []string
	for _, ref := range PersonRefs(value) {
		if ref != OwnerRef {
			ok, err := v.personVisible(ref, allow)
			if err != nil {
				return "", err
			}
			if !ok {
				continue
			}
		}
		kept = append(kept, ref)
	}
	return strings.Join(kept, ","), nil
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

// isPersonRefID is isPersonRefField for a field ID without its person prefix.
func isPersonRefID(local string) bool {
	parts := strings.SplitN(canonicalLocalID(local), ".", 2)
	return len(parts) == 2 && isPersonRefField(parts[0], parts[1])
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

// RemovePerson deletes a person with every field and document of theirs, in
// one transaction, and revokes the service tokens and scoped grants whose
// scope names them, so a later person with the same slug can't inherit them.
// It refuses while another field names the person (PersonInUseError).
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

	if exists, err := v.db.PersonExists(slug); err != nil || !exists {
		if err == nil {
			err = ErrPersonNotFound
		}
		return nil, err
	}
	fieldIDs, docIDs, err := v.db.PersonRows(slug)
	if err != nil {
		return nil, err
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
	// The nucleo migration's record of who a membro became goes too, or a
	// later person with this slug would get that membro's leftover fields.
	migrated, err := v.db.MetaWithPrefix(metaNucleoMembro)
	if err != nil {
		return nil, err
	}
	var forget []string
	for k, s := range migrated {
		if s == slug {
			forget = append(forget, k)
		}
	}
	if err := v.db.ApplyChange(store.Change{DeleteFields: fieldIDs, DeleteDocs: docIDs, DeleteTokens: revoke, DeletePerson: slug, DeleteMeta: forget}); err != nil {
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

// personRefsTo returns the IDs of fields outside the person's own data whose
// value names slug. A closed pratica (completata, respinta, annullata) is
// history, not a reference: it doesn't keep a person from being removed.
func (v *Vault) personRefsTo(slug string) ([]string, error) {
	ctx, err := v.decryptFields(func(id, _ string) bool {
		person, local := SplitPerson(id)
		parts := strings.SplitN(canonicalLocalID(local), ".", 2) // famiglia2.genitori names people too
		if person == slug || len(parts) != 2 {
			return false
		}
		_, campo, _ := praticaField(parts[0], parts[1])
		return isPersonRefField(parts[0], parts[1]) || campo == "stato"
	})
	if err != nil {
		return nil, err
	}
	pratiche := praticheIn(ctx, nil)
	var refs []string
	for _, fs := range ctx.Categories {
		for _, f := range fs {
			pslug, campo, isPratica := praticaField(f.Category, f.FieldName)
			if campo == "stato" || (isPratica && pratiche[pslug] != nil && pratiche[pslug].closed()) {
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
