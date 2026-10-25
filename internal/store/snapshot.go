package store

import "database/sql"

// Snapshot writes a consistent copy of the database to path, which must not
// exist yet. It is safe to call while other connections are writing.
func (d *DB) Snapshot(path string) error {
	_, err := d.conn.Exec("VACUUM INTO ?", path)
	return err
}

// SnapshotDocumentIDs lists the document IDs recorded in a database file
// written by Snapshot, without changing the file.
func SnapshotDocumentIDs(path string) ([]string, error) {
	if err := checkPath(path); err != nil {
		return nil, err
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	rows, err := conn.Query("SELECT id FROM vault_documents ORDER BY id")
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

// PrepareSnapshot readies a snapshot file for a backup: it removes the rows
// of documents in dropDocs (e.g. deleted after it was taken) and every
// service token, so a restore never brings back one revoked since, and sets
// the meta entries in setMeta.
func PrepareSnapshot(path string, dropDocs []string, setMeta map[string]string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	// Write straight to the file (no WAL), so the change is in it once closed.
	if _, err := conn.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		return err
	}
	for _, id := range dropDocs {
		if _, err := conn.Exec("DELETE FROM vault_documents WHERE id = ?", id); err != nil {
			return err
		}
	}
	if _, err := conn.Exec("DELETE FROM vault_tokens"); err != nil {
		return err
	}
	for k, v := range setMeta {
		if _, err := conn.Exec("INSERT INTO vault_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", k, v); err != nil {
			return err
		}
	}
	return nil
}
