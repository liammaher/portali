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

// SnapshotDropDocuments removes document rows from a snapshot file, e.g. for
// documents deleted after it was taken.
func SnapshotDropDocuments(path string, ids []string) error {
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
	for _, id := range ids {
		if _, err := conn.Exec("DELETE FROM vault_documents WHERE id = ?", id); err != nil {
			return err
		}
	}
	return nil
}
