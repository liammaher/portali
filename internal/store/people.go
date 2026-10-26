package store

import (
	"database/sql"
	"time"
)

// Other people's fields and documents are stored in categories prefixed
// with the person's slug and a colon (leo:cie), and the person in
// vault_people. ';' sorts right after ':', so [slug:, slug;) is exactly one
// person's categories, without LIKE (where '_' would be a wildcard).

// PersonExists reports whether the vault has the person.
func (d *DB) PersonExists(slug string) (bool, error) {
	var n int
	err := d.conn.QueryRow(`SELECT EXISTS (SELECT 1 FROM vault_people WHERE slug = ?)`, slug).Scan(&n)
	return n == 1, err
}

// PersonSlugs returns the slugs of the vault's people, sorted.
func (d *DB) PersonSlugs() ([]string, error) {
	return d.ids(`SELECT slug FROM vault_people ORDER BY slug`)
}

// PersonRows returns the IDs of a person's fields and documents.
func (d *DB) PersonRows(slug string) (fields, docs []string, err error) {
	lo, hi := slug+":", slug+";"
	if fields, err = d.ids(`SELECT id FROM vault_fields WHERE category >= ? AND category < ? ORDER BY id`, lo, hi); err != nil {
		return nil, nil, err
	}
	docs, err = d.ids(`SELECT id FROM vault_documents WHERE category >= ? AND category < ? ORDER BY id`, lo, hi)
	return fields, docs, err
}

// PersonRowTiers returns a person's fields (ID and Sensitivity set) and
// documents (ID, Category and Sensitivity set).
func (d *DB) PersonRowTiers(slug string) (fields []Field, docs []Document, err error) {
	lo, hi := slug+":", slug+";"
	rows, err := d.conn.Query(`SELECT id, sensitivity FROM vault_fields WHERE category >= ? AND category < ?`, lo, hi)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f Field
		if err := rows.Scan(&f.ID, &f.Sensitivity); err != nil {
			return nil, nil, err
		}
		fields = append(fields, f)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	drows, err := d.conn.Query(`SELECT id, category, sensitivity FROM vault_documents WHERE category >= ? AND category < ?`, lo, hi)
	if err != nil {
		return nil, nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var doc Document
		if err := drows.Scan(&doc.ID, &doc.Category, &doc.Sensitivity); err != nil {
			return nil, nil, err
		}
		docs = append(docs, doc)
	}
	return fields, docs, drows.Err()
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

// Change is a set of row changes ApplyChange makes in one transaction.
type Change struct {
	DeleteFields, DeleteDocs []string // each must still exist
	DeleteTokens             []string // a token already gone (expired, revoked) is no error
	Insert                   []Field  // each must not exist yet
	DeletePerson             string   // the person removed with their rows, if any
}

// ApplyChange makes c in one transaction, so removing or migrating a person
// never half-happens. If a row to delete is gone or one to insert exists,
// nothing changes and it returns ErrChanged.
func (d *DB) ApplyChange(c Change) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range c.DeleteFields {
		if err := oneRow(tx.Exec(`DELETE FROM vault_fields WHERE id = ?`, id)); err != nil {
			return err
		}
	}
	for _, id := range c.DeleteDocs {
		if err := oneRow(tx.Exec(`DELETE FROM vault_documents WHERE id = ?`, id)); err != nil {
			return err
		}
	}
	for _, token := range c.DeleteTokens {
		if _, err := tx.Exec(`DELETE FROM vault_tokens WHERE token = ?`, token); err != nil {
			return err
		}
	}
	for _, f := range c.Insert {
		var closedAt any // NULL unless set
		if !f.ClosedAt.IsZero() {
			closedAt = f.ClosedAt.UTC().Format(time.RFC3339)
		}
		res, err := tx.Exec(
			`INSERT INTO vault_fields (id, category, field_name, value, sensitivity, updated_at, version, closed_at)
			 VALUES (?, ?, ?, ?, ?, ?, 1, ?) ON CONFLICT(id) DO NOTHING`,
			f.ID, f.Category, f.FieldName, f.Value, f.Sensitivity, f.UpdatedAt.UTC().Format(time.RFC3339), closedAt,
		)
		if err := oneRow(res, err); err != nil {
			return err
		}
	}
	if c.DeletePerson != "" {
		if err := oneRow(tx.Exec(`DELETE FROM vault_people WHERE slug = ?`, c.DeletePerson)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// oneRow is affectedOne for a checked change.
func oneRow(res sql.Result, err error) error {
	return affectedOne(res, err, true)
}
