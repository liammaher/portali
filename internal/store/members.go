package store

import (
	"database/sql"
	"sort"
	"time"
)

// Household members' fields and documents are stored in categories
// prefixed with the member's slug and a colon (leo:cie). ';' sorts right
// after ':', so [slug:, slug;) is exactly one member's categories, without
// LIKE (where '_' would be a wildcard).

// MemberExists reports whether a member has any field or document.
func (d *DB) MemberExists(slug string) (bool, error) {
	lo, hi := slug+":", slug+";"
	var n int
	err := d.conn.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM vault_fields WHERE category >= ? AND category < ?)
		     OR EXISTS (SELECT 1 FROM vault_documents WHERE category >= ? AND category < ?)`,
		lo, hi, lo, hi,
	).Scan(&n)
	return n == 1, err
}

// MemberSlugs returns the slugs of every member with a field or document,
// sorted.
func (d *DB) MemberSlugs() ([]string, error) {
	rows, err := d.conn.Query(
		`SELECT DISTINCT substr(category, 1, instr(category, ':') - 1) FROM vault_fields WHERE instr(category, ':') > 0
		 UNION
		 SELECT DISTINCT substr(category, 1, instr(category, ':') - 1) FROM vault_documents WHERE instr(category, ':') > 0`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var slugs []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return slugs, rows.Err()
}

// MemberRows returns the IDs of a member's fields and documents.
func (d *DB) MemberRows(slug string) (fields, docs []string, err error) {
	lo, hi := slug+":", slug+";"
	if fields, err = d.ids(`SELECT id FROM vault_fields WHERE category >= ? AND category < ? ORDER BY id`, lo, hi); err != nil {
		return nil, nil, err
	}
	docs, err = d.ids(`SELECT id FROM vault_documents WHERE category >= ? AND category < ? ORDER BY id`, lo, hi)
	return fields, docs, err
}

func (d *DB) ids(query string, args ...any) ([]string, error) {
	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ApplyMemberChange deletes fields and documents and inserts new fields in
// one transaction, so removing or migrating a member never half-happens.
// Every row to delete must still exist and every field to insert must not
// exist yet; otherwise nothing changes and it returns ErrChanged.
func (d *DB) ApplyMemberChange(deleteFields, deleteDocs []string, insert []Field) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range deleteFields {
		if err := oneRow(tx.Exec(`DELETE FROM vault_fields WHERE id = ?`, id)); err != nil {
			return err
		}
	}
	for _, id := range deleteDocs {
		if err := oneRow(tx.Exec(`DELETE FROM vault_documents WHERE id = ?`, id)); err != nil {
			return err
		}
	}
	for _, f := range insert {
		res, err := tx.Exec(
			`INSERT INTO vault_fields (id, category, field_name, value, sensitivity, updated_at, version)
			 VALUES (?, ?, ?, ?, ?, ?, 1) ON CONFLICT(id) DO NOTHING`,
			f.ID, f.Category, f.FieldName, f.Value, f.Sensitivity, f.UpdatedAt.UTC().Format(time.RFC3339),
		)
		if err := oneRow(res, err); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// oneRow is affectedOne for a checked change.
func oneRow(res sql.Result, err error) error {
	return affectedOne(res, err, true)
}
