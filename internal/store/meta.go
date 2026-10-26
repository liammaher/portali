package store

import "database/sql"

// SetMeta upserts a key-value pair in vault_meta.
func (d *DB) SetMeta(key, value string) error {
	_, err := d.conn.Exec(
		`INSERT INTO vault_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// GetMeta retrieves a value by key. Returns empty string if not found.
func (d *DB) GetMeta(key string) (string, error) {
	var value string
	err := d.conn.QueryRow("SELECT value FROM vault_meta WHERE key = ?", key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

// SetMetas upserts several key-value pairs in one transaction.
func (d *DB) SetMetas(pairs map[string]string) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range pairs {
		if _, err := tx.Exec(`INSERT INTO vault_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MetaWithPrefix returns the vault_meta pairs whose key starts with prefix.
func (d *DB) MetaWithPrefix(prefix string) (map[string]string, error) {
	rows, err := d.conn.Query("SELECT key, value FROM vault_meta WHERE substr(key, 1, ?) = ?", len(prefix), prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// IsInitialized checks if the vault has been initialized.
func (d *DB) IsInitialized() (bool, error) {
	salt, err := d.GetMeta("salt")
	if err != nil {
		return false, err
	}
	return salt != "", nil
}
