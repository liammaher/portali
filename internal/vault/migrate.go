package vault

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/store"
)

// migrateLegacyFields brings fields written by older versions up to today's
// rules. It needs the key to read and re-encrypt them, so Unlock runs it:
//   - field IDs with upper-case letters (from before CheckLowerCase) become
//     lower-case (lowerCaseFieldIDs), so rules read stored IDs as they are;
//   - each pratica's closing time is made to agree with its stato
//     (dateClosedPratiche).
func (v *Vault) migrateLegacyFields() error {
	v.backup.mu.Lock()
	defer v.backup.mu.Unlock()
	v.writeMu.Lock()
	defer v.writeMu.Unlock()
	return errors.Join(v.lowerCaseFieldIDs(), v.dateClosedPratiche())
}

// lowerCaseFieldIDs re-encrypts every field whose ID has upper-case letters
// under its lower-case ID, since the category is the HKDF info. Where several
// spellings of one ID exist, the one written last wins, at the highest tier
// any of them had. An ID with a spelling it can't decrypt stays as it is, and
// the error names it; the others still move. Document categories keep their
// case: moving one would mean rewriting its file, and the rules read them
// with schemaCategoryOf.
func (v *Vault) lowerCaseFieldIDs() error {
	fields, err := v.db.ListFields()
	if err != nil {
		return err
	}
	stored := map[string]store.Field{} // lower-case IDs as stored
	legacy := false
	for _, f := range fields {
		if f.ID == strings.ToLower(f.ID) {
			stored[f.ID] = f
		} else {
			legacy = true
		}
	}
	if !legacy {
		return nil
	}
	vaultKey, err := v.requireUnlocked()
	if err != nil {
		return err
	}
	defer clear(vaultKey)
	all, err := v.db.GetAllFields()
	if err != nil {
		return err
	}
	spellings := map[string][]FieldInfo{} // lower-case ID -> its legacy spellings
	var unreadable []string
	for _, f := range all {
		lower := strings.ToLower(f.ID)
		if f.ID == lower {
			continue
		}
		value, err := decryptValue(vaultKey, v.salt, f.Category, f.Value)
		if err != nil {
			unreadable = append(unreadable, f.ID)
			continue
		}
		spellings[lower] = append(spellings[lower], fieldInfo(f, value))
	}
	for _, id := range unreadable {
		delete(spellings, strings.ToLower(id)) // its spellings stay together
	}
	var skipped error
	if len(unreadable) > 0 {
		skipped = fmt.Errorf("left with upper-case IDs, can't decrypt: %s", strings.Join(unreadable, ", "))
	}
	if len(spellings) == 0 {
		return skipped
	}

	var deletes []string
	var inserts []store.Field
	ids := make([]string, 0, len(spellings))
	for id := range spellings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rows := spellings[id]
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		// The highest tier of any spelling: public ranks 0, like "", so start
		// from a real one or a public field would come out with none.
		winner, tier := rows[0], rows[0].Sensitivity
		for _, f := range rows {
			deletes = append(deletes, f.ID)
			if f.UpdatedAt.After(winner.UpdatedAt) {
				winner = f
			}
			if tierRank[f.Sensitivity] > tierRank[tier] {
				tier = f.Sensitivity
			}
		}
		if current, ok := stored[id]; ok {
			if tierRank[current.Sensitivity] > tierRank[tier] {
				tier = current.Sensitivity
			}
			if !winner.UpdatedAt.After(current.UpdatedAt) {
				if tier != current.Sensitivity {
					if err := v.db.RaiseTier(tier, []string{id}, nil); err != nil {
						return err
					}
				}
				continue // the lower-case row is the latest: keep it
			}
			deletes = append(deletes, id)
		}
		category, fieldName, _ := strings.Cut(id, ".")
		encrypted, err := encryptValue(vaultKey, v.salt, category, winner.Value)
		if err != nil {
			return err
		}
		inserts = append(inserts, store.Field{ID: id, Category: category, FieldName: fieldName,
			Value: encrypted, Sensitivity: tier, UpdatedAt: winner.UpdatedAt, ClosedAt: winner.ClosedAt})
	}
	if err := v.db.ApplyChange(store.Change{DeleteFields: deletes, Insert: inserts}); err != nil {
		return err
	}
	v.db.LogAccess(store.AuditEntry{Consumer: "vault", Scope: strings.Join(ids, ","), Action: "case_migrate",
		Purpose: fmt.Sprintf("%d field IDs made lower-case", len(ids))})
	v.markChanged()
	return skipped
}

// dateClosedPratiche makes every pratica's closing time agree with its
// stato, for vaults an older version wrote to (it didn't know closed_at): a
// closed stato without one is dated at its last write, and an open one
// loses a stale one, so a later closing is dated afresh.
func (v *Vault) dateClosedPratiche() error {
	ctx, err := v.decryptFields(func(id, _ string) bool {
		category, name, _ := strings.Cut(id, ".")
		_, campo, _ := praticaField(category, name)
		return campo == "stato"
	})
	if err != nil {
		return err
	}
	var undated, stale []string
	for _, f := range ctx.Categories["pratiche"] {
		switch closed := isClosedStato(f.Value); {
		case closed && f.ClosedAt.IsZero():
			undated = append(undated, f.ID)
		case !closed && !f.ClosedAt.IsZero():
			stale = append(stale, f.ID)
		}
	}
	if len(undated) == 0 && len(stale) == 0 {
		return nil
	}
	return v.db.FixClosedAt(undated, stale)
}
