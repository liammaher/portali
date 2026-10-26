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
	// ClosedAt is when a pratica's stato last went from open to closed, zero
	// for any other field. A write sets it from Closes, in the same
	// statement: kept while the stato stays closed, cleared when it reopens.
	ClosedAt time.Time
	Closes   bool
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
			`INSERT INTO vault_fields (id, category, field_name, value, sensitivity, updated_at, version, closed_at)
			 VALUES (?, ?, ?, ?, ?, ?, 1, CASE WHEN ? THEN ? END)
			 ON CONFLICT(id) DO UPDATE SET
				value = excluded.value,
				sensitivity = CASE WHEN ? OR excluded.sensitivity = '' THEN vault_fields.sensitivity ELSE excluded.sensitivity END,
				updated_at = excluded.updated_at,
				version = vault_fields.version + 1,
				closed_at = CASE WHEN excluded.closed_at IS NOT NULL THEN COALESCE(vault_fields.closed_at, excluded.closed_at) END`,
			f.ID, f.Category, f.FieldName, f.Value, f.Sensitivity, updated, f.Closes, updated, f.KeepTier,
		)
	case !f.Seen.Exists:
		res, err = d.conn.Exec(
			`INSERT INTO vault_fields (id, category, field_name, value, sensitivity, updated_at, version, closed_at)
			 VALUES (?, ?, ?, ?, ?, ?, 1, CASE WHEN ? THEN ? END) ON CONFLICT(id) DO NOTHING`,
			f.ID, f.Category, f.FieldName, f.Value, f.Sensitivity, updated, f.Closes, updated,
		)
	default:
		res, err = d.conn.Exec(
			`UPDATE vault_fields SET value = ?,
				sensitivity = CASE WHEN ? OR ? = '' THEN sensitivity ELSE ? END,
				updated_at = ?, version = version + 1,
				closed_at = CASE WHEN ? THEN COALESCE(closed_at, ?) END
			 WHERE id = ? AND sensitivity = ?`,
			f.Value, f.KeepTier, f.Sensitivity, f.Sensitivity, updated, f.Closes, updated, f.ID, f.Seen.Tier,
		)
	}
	return affectedOne(res, err, true) // an unchecked upsert always writes
}

// FixClosedAt corrects closing times an older version left behind (it
// didn't know the column): a closed stato without one is dated at its
// updated_at, and an open one loses its stale one.
func (d *DB) FixClosedAt(undated, stale []string) error {
	for _, id := range undated {
		if _, err := d.conn.Exec(`UPDATE vault_fields SET closed_at = updated_at WHERE id = ? AND closed_at IS NULL`, id); err != nil {
			return err
		}
	}
	for _, id := range stale {
		if _, err := d.conn.Exec(`UPDATE vault_fields SET closed_at = NULL WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// GetField retrieves a single field by ID (includes encrypted value).
func (d *DB) GetField(id string) (*Field, error) {
	fields, err := d.queryFields(true, "WHERE id = ?", id)
	if err != nil || len(fields) == 0 {
		return nil, err
	}
	return &fields[0], nil
}

// ListFields returns all field metadata (no values).
func (d *DB) ListFields() ([]Field, error) {
	return d.queryFields(false, "ORDER BY category, field_name")
}

// ListFieldsByCategory returns field metadata for a category (no values).
func (d *DB) ListFieldsByCategory(category string) ([]Field, error) {
	return d.queryFields(false, "WHERE category = ? ORDER BY field_name", category)
}

// GetFieldsByCategory returns fields in a category (with encrypted values).
func (d *DB) GetFieldsByCategory(category string) ([]Field, error) {
	return d.queryFields(true, "WHERE category = ? ORDER BY field_name", category)
}

// GetAllFields returns all fields including encrypted values.
func (d *DB) GetAllFields() ([]Field, error) {
	return d.queryFields(true, "ORDER BY category, field_name")
}

// queryFields returns the fields a WHERE/ORDER BY clause selects, with their
// encrypted values if withValue.
func (d *DB) queryFields(withValue bool, clause string, args ...any) ([]Field, error) {
	value := "''"
	if withValue {
		value = "value"
	}
	rows, err := d.conn.Query("SELECT id, category, field_name, "+value+", sensitivity, updated_at, version, closed_at FROM vault_fields "+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fields []Field
	for rows.Next() {
		var f Field
		var updatedAt string
		var closedAt sql.NullString
		if err := rows.Scan(&f.ID, &f.Category, &f.FieldName, &f.Value, &f.Sensitivity, &updatedAt, &f.Version, &closedAt); err != nil {
			return nil, err
		}
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		if closedAt.Valid {
			f.ClosedAt, _ = time.Parse(time.RFC3339, closedAt.String)
		}
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
	// updated_at is when the value last changed, so a tier change leaves it.
	res, err := d.conn.Exec("UPDATE vault_fields SET sensitivity = ? WHERE id = ?", tier, id)
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

// FieldCount returns the number of fields, only the owner's with ownerOnly
// (other people's left out). Pass a tier as exclude to leave fields of
// that tier out ("" counts all).
func (d *DB) FieldCount(exclude string, ownerOnly bool) (int, error) {
	var count int
	err := d.conn.QueryRow("SELECT COUNT(*) FROM vault_fields WHERE (? = '' OR sensitivity != ?) AND (NOT ? OR instr(category, ':') = 0)", exclude, exclude, ownerOnly).Scan(&count)
	return count, err
}

// CategoryCounts returns a map of the owner's category -> field count
// (other people's categories are left out). Pass a tier as exclude to
// leave fields of that tier out ("" counts all).
func (d *DB) CategoryCounts(exclude string) (map[string]int, error) {
	rows, err := d.conn.Query("SELECT category, COUNT(*) FROM vault_fields WHERE (? = '' OR sensitivity != ?) AND instr(category, ':') = 0 GROUP BY category", exclude, exclude)
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

// RaiseTier sets tier on the given fields and documents in one transaction,
// leaving updated_at alone: it corrects a classification, not the data.
func (d *DB) RaiseTier(tier string, fieldIDs, docIDs []string) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range fieldIDs {
		if _, err := tx.Exec(`UPDATE vault_fields SET sensitivity = ? WHERE id = ?`, tier, id); err != nil {
			return err
		}
	}
	for _, id := range docIDs {
		if _, err := tx.Exec(`UPDATE vault_documents SET sensitivity = ? WHERE id = ?`, tier, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
