package store

import (
	"database/sql"
	"time"
)

// Document is a row in vault_documents. The file content lives encrypted on
// disk; Name holds the encrypted original filename.
type Document struct {
	ID          string
	Category    string
	Name        string // encrypted original filename (base64)
	ContentType string
	Size        int64
	Sensitivity string
	CreatedAt   time.Time
}

// CreateDocument inserts document metadata.
func (d *DB) CreateDocument(doc Document) error {
	_, err := d.conn.Exec(
		`INSERT INTO vault_documents (id, category, name, content_type, size, sensitivity, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.Category, doc.Name, doc.ContentType, doc.Size, doc.Sensitivity,
		doc.CreatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// GetDocument returns document metadata, or nil if it doesn't exist.
func (d *DB) GetDocument(id string) (*Document, error) {
	row := d.conn.QueryRow(
		"SELECT id, category, name, content_type, size, sensitivity, created_at FROM vault_documents WHERE id = ?", id,
	)
	doc, err := scanDocument(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return doc, err
}

// ListDocuments returns all document metadata, newest first.
func (d *DB) ListDocuments() ([]Document, error) {
	rows, err := d.conn.Query(
		"SELECT id, category, name, content_type, size, sensitivity, created_at FROM vault_documents ORDER BY category, created_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, *doc)
	}
	return docs, rows.Err()
}

// DeleteDocument removes document metadata. Returns the number of rows deleted.
func (d *DB) DeleteDocument(id string) (int64, error) {
	result, err := d.conn.Exec("DELETE FROM vault_documents WHERE id = ?", id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DocumentCount returns the number of stored documents.
func (d *DB) DocumentCount() (int, error) {
	var n int
	err := d.conn.QueryRow("SELECT COUNT(*) FROM vault_documents").Scan(&n)
	return n, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDocument(r rowScanner) (*Document, error) {
	var doc Document
	var createdAt string
	if err := r.Scan(&doc.ID, &doc.Category, &doc.Name, &doc.ContentType, &doc.Size, &doc.Sensitivity, &createdAt); err != nil {
		return nil, err
	}
	doc.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &doc, nil
}
