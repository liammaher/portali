package store

import (
	"database/sql"
	"errors"
	"time"
)

var (
	// ErrNotFound means the field doesn't exist.
	ErrNotFound = errors.New("field not found")
	// ErrChanged means the field changed after the caller checked it.
	ErrChanged = errors.New("field changed while this request was checked; try again")
)

// Seen is what a caller checked about a field before changing it: whether
// it existed and with which tier. A write or delete given a Seen happens
// only if the row still matches, so a concurrent change can't slip past
// the check (ErrChanged).
type Seen struct {
	Exists bool
	Tier   string
}

// Field represents a row in vault_fields.
type Field struct {
	ID          string
	Category    string
	FieldName   string
	Value       string // encrypted ciphertext (base64)
	Sensitivity string
	UpdatedAt   time.Time
	Version     int
	// KeepTier makes an update keep the stored tier; Sensitivity is then
	// only used when the field is new. Deciding in the same statement means
	// a concurrent tier change is never overwritten with a stale one.
	KeepTier bool
	// Seen, if set, makes the write conditional on it (see Seen).
	Seen *Seen
}

// SetField upserts a field. If the field exists, bumps version.
func (d *DB) SetField(f Field) error {
	updated := f.UpdatedAt.UTC().Format(time.RFC3339)
	var res sql.Result
	var err error
	switch {
	case f.Seen == nil:
		res, err = d.conn.Exec(
			`INSERT INTO vault_fields (id, category, field_name, value, sensitivity, updated_at, version)
			 VALUES (?, ?, ?, ?, ?, ?, 1)
			 ON CONFLICT(id) DO UPDATE SET
				value = excluded.value,
				sensitivity = CASE WHEN ? OR excluded.sensitivity = '' THEN vault_fields.sensitivity ELSE excluded.sensitivity END,
				updated_at = excluded.updated_at,
				version = vault_fields.version + 1`,
			f.ID, f.Category, f.FieldName, f.Value, f.Sensitivity, updated, f.KeepTier,
		)
	case !f.Seen.Exists:
		res, err = d.conn.Exec(
			`INSERT INTO vault_fields (id, category, field_name, value, sensitivity, updated_at, version)
			 VALUES (?, ?, ?, ?, ?, ?, 1) ON CONFLICT(id) DO NOTHING`,
			f.ID, f.Category, f.FieldName, f.Value, f.Sensitivity, updated,
		)
	default:
		res, err = d.conn.Exec(
			`UPDATE vault_fields SET value = ?,
				sensitivity = CASE WHEN ? OR ? = '' THEN sensitivity ELSE ? END,
				updated_at = ?, version = version + 1
			 WHERE id = ? AND sensitivity = ?`,
			f.Value, f.KeepTier, f.Sensitivity, f.Sensitivity, updated, f.ID, f.Seen.Tier,
		)
	}
	return affectedOne(res, err, true) // an unchecked upsert always writes
}

// GetField retrieves a single field by ID (includes encrypted value).
func (d *DB) GetField(id string) (*Field, error) {
	var f Field
	var updatedAt string
	err := d.conn.QueryRow(
		"SELECT id, category, field_name, value, sensitivity, updated_at, version FROM vault_fields WHERE id = ?",
		id,
	).Scan(&f.ID, &f.Category, &f.FieldName, &f.Value, &f.Sensitivity, &updatedAt, &f.Version)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &f, nil
}

// ListFields returns all field metadata (no values).
func (d *DB) ListFields() ([]Field, error) {
	rows, err := d.conn.Query(
		"SELECT id, category, field_name, sensitivity, updated_at, version FROM vault_fields ORDER BY category, field_name",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fields []Field
	for rows.Next() {
		var f Field
		var updatedAt string
		if err := rows.Scan(&f.ID, &f.Category, &f.FieldName, &f.Sensitivity, &updatedAt, &f.Version); err != nil {
			return nil, err
		}
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

// ListFieldsByCategory returns field metadata for a category (no values).
func (d *DB) ListFieldsByCategory(category string) ([]Field, error) {
	rows, err := d.conn.Query(
		"SELECT id, category, field_name, sensitivity, updated_at, version FROM vault_fields WHERE category = ? ORDER BY field_name",
		category,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fields []Field
	for rows.Next() {
		var f Field
		var updatedAt string
		if err := rows.Scan(&f.ID, &f.Category, &f.FieldName, &f.Sensitivity, &updatedAt, &f.Version); err != nil {
			return nil, err
		}
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

// GetFieldsByCategory returns fields in a category (with encrypted values).
func (d *DB) GetFieldsByCategory(category string) ([]Field, error) {
	rows, err := d.conn.Query(
		"SELECT id, category, field_name, value, sensitivity, updated_at, version FROM vault_fields WHERE category = ? ORDER BY field_name",
		category,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fields []Field
	for rows.Next() {
		var f Field
		var updatedAt string
		if err := rows.Scan(&f.ID, &f.Category, &f.FieldName, &f.Value, &f.Sensitivity, &updatedAt, &f.Version); err != nil {
			return nil, err
		}
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

// GetAllFields returns all fields including encrypted values.
func (d *DB) GetAllFields() ([]Field, error) {
	rows, err := d.conn.Query(
		"SELECT id, category, field_name, value, sensitivity, updated_at, version FROM vault_fields ORDER BY category, field_name",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fields []Field
	for rows.Next() {
		var f Field
		var updatedAt string
		if err := rows.Scan(&f.ID, &f.Category, &f.FieldName, &f.Value, &f.Sensitivity, &updatedAt, &f.Version); err != nil {
			return nil, err
		}
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

// DeleteField removes a field by ID: ErrNotFound if there is none, or,
// given a Seen, ErrChanged if its tier is no longer the one checked.
func (d *DB) DeleteField(id string, seen *Seen) error {
	var res sql.Result
	var err error
	if seen == nil {
		res, err = d.conn.Exec("DELETE FROM vault_fields WHERE id = ?", id)
	} else {
		res, err = d.conn.Exec("DELETE FROM vault_fields WHERE id = ? AND sensitivity = ?", id, seen.Tier)
	}
	return affectedOne(res, err, seen != nil)
}

// SetSensitivity updates the sensitivity tier of a field (ErrNotFound if
// there is none).
func (d *DB) SetSensitivity(id, tier string) error {
	res, err := d.conn.Exec(
		"UPDATE vault_fields SET sensitivity = ?, updated_at = ? WHERE id = ?",
		tier, time.Now().UTC().Format(time.RFC3339), id,
	)
	return affectedOne(res, err, false)
}

// affectedOne turns a statement that changed no row into ErrChanged (for a
// checked change) or ErrNotFound.
func affectedOne(res sql.Result, err error, checked bool) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	switch {
	case err != nil:
		return err
	case n > 0:
		return nil
	case checked:
		return ErrChanged
	}
	return ErrNotFound
}

// FieldCount returns total number of fields.
// Pass a tier as exclude to leave fields of that tier out ("" counts all).
func (d *DB) FieldCount(exclude string) (int, error) {
	var count int
	err := d.conn.QueryRow("SELECT COUNT(*) FROM vault_fields WHERE ? = '' OR sensitivity != ?", exclude, exclude).Scan(&count)
	return count, err
}

// CategoryCounts returns a map of category -> field count.
// Pass a tier as exclude to leave fields of that tier out ("" counts all).
func (d *DB) CategoryCounts(exclude string) (map[string]int, error) {
	rows, err := d.conn.Query("SELECT category, COUNT(*) FROM vault_fields WHERE ? = '' OR sensitivity != ? GROUP BY category", exclude, exclude)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var cat string
		var count int
		if err := rows.Scan(&cat, &count); err != nil {
			return nil, err
		}
		counts[cat] = count
	}
	return counts, rows.Err()
}
