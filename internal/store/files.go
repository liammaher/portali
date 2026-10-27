package store

import (
	"database/sql"
	"time"
)

// The file index. Every Blob is ciphertext made by the vault layer (files.go
// there); this package only keeps rows and the few plain columns scope checks
// need. Nothing here deletes a row on its own: a file that left its source is
// tombstoned (GoneAt), and removing a source or a person is an explicit call.

// FileSource is a place files are read from: a folder on disk or an account.
type FileSource struct {
	ID        string
	Provider  string
	Blob      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// File is one logical file. Blob holds its name and where it is.
type File struct {
	ID        string
	SourceID  string
	Tier      string
	Blob      string
	GoneAt    time.Time // zero while the file is still at its source
	Scoped    bool      // was linked to a person once: not the owner's, even with no link left
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FileLink ties a file to a holder: "" for the owner or business, else a
// person's slug. A pratica link also names the pratica.
type FileLink struct {
	ID          string
	FileID      string
	Holder      string
	Kind        string
	PraticaSlug string
	Origin      string
	Tier        string
	Blob        string
	CreatedAt   time.Time
}

// FileRule links every file under a path of a source, as it is indexed.
type FileRule struct {
	ID          string
	SourceID    string
	Holder      string
	Kind        string
	PraticaSlug string
	Blob        string
	CreatedAt   time.Time
}

// FileSuggestion is a link an agent proposed and the user has yet to accept.
type FileSuggestion struct {
	ID          string
	FileID      string
	Holder      string
	Kind        string
	PraticaSlug string
	ProposedBy  string
	Blob        string
	CreatedAt   time.Time
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// CreateFileSource inserts a source.
func (d *DB) CreateFileSource(s FileSource) error {
	_, err := d.conn.Exec(
		`INSERT INTO vault_file_sources (id, provider, blob, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		s.ID, s.Provider, s.Blob, ts(s.CreatedAt), ts(s.UpdatedAt))
	return err
}

// UpdateFileSource replaces a source's blob (its cursor, last sync, ...).
func (d *DB) UpdateFileSource(id, blob string, at time.Time) error {
	return oneRow(d.conn.Exec(`UPDATE vault_file_sources SET blob = ?, updated_at = ? WHERE id = ?`, blob, ts(at), id))
}

// GetFileSource returns a source, or nil.
func (d *DB) GetFileSource(id string) (*FileSource, error) {
	var s FileSource
	var created, updated string
	err := d.conn.QueryRow(`SELECT id, provider, blob, created_at, updated_at FROM vault_file_sources WHERE id = ?`, id).
		Scan(&s.ID, &s.Provider, &s.Blob, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.CreatedAt, s.UpdatedAt = parseTS(created), parseTS(updated)
	return &s, nil
}

// ListFileSources returns every source, oldest first.
func (d *DB) ListFileSources() ([]FileSource, error) {
	rows, err := d.conn.Query(`SELECT id, provider, blob, created_at, updated_at FROM vault_file_sources ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileSource
	for rows.Next() {
		var s FileSource
		var created, updated string
		if err := rows.Scan(&s.ID, &s.Provider, &s.Blob, &created, &updated); err != nil {
			return nil, err
		}
		s.CreatedAt, s.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteFileSource forgets a source with its files, their links, its rules and
// the suggestions about its files, in one transaction. It returns how many
// files went.
func (d *DB) DeleteFileSource(id string) (files int64, err error) {
	tx, err := d.conn.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM vault_file_links WHERE file_id IN (SELECT id FROM vault_files WHERE source_id = ?)`,
		`DELETE FROM vault_file_suggestions WHERE file_id IN (SELECT id FROM vault_files WHERE source_id = ?)`,
		`DELETE FROM vault_file_rules WHERE source_id = ?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return 0, err
		}
	}
	res, err := tx.Exec(`DELETE FROM vault_files WHERE source_id = ?`, id)
	if err != nil {
		return 0, err
	}
	files, _ = res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM vault_file_sources WHERE id = ?`, id); err != nil {
		return 0, err
	}
	return files, tx.Commit()
}

// CreateFile inserts a file.
func (d *DB) CreateFile(f File) error {
	_, err := d.conn.Exec(
		`INSERT INTO vault_files (id, source_id, tier, blob, gone_at, scoped, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.SourceID, f.Tier, f.Blob, goneAt(f.GoneAt), f.Scoped, ts(f.CreatedAt), ts(f.UpdatedAt))
	return err
}

// UpdateFile replaces a file's blob, tier and tombstone.
func (d *DB) UpdateFile(f File) error {
	return oneRow(d.conn.Exec(
		`UPDATE vault_files SET tier = ?, blob = ?, gone_at = ?, updated_at = ? WHERE id = ?`,
		f.Tier, f.Blob, goneAt(f.GoneAt), ts(f.UpdatedAt), f.ID))
}

// SetFileScoped marks a file as having been linked to a person.
func (d *DB) SetFileScoped(id string) error {
	return oneRow(d.conn.Exec(`UPDATE vault_files SET scoped = 1 WHERE id = ?`, id))
}

// ScopedFileIDs returns the files marked by SetFileScoped.
func (d *DB) ScopedFileIDs() (map[string]bool, error) {
	ids, err := d.ids(`SELECT id FROM vault_files WHERE scoped = 1`)
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// CountFilesBySource returns how many files each source has.
func (d *DB) CountFilesBySource() (map[string]int, error) {
	rows, err := d.conn.Query(`SELECT source_id, COUNT(*) FROM vault_files GROUP BY source_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func goneAt(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return ts(t)
}

const fileCols = `id, source_id, tier, blob, gone_at, scoped, created_at, updated_at`

func scanFile(r rowScanner) (*File, error) {
	var f File
	var gone sql.NullString
	var created, updated string
	if err := r.Scan(&f.ID, &f.SourceID, &f.Tier, &f.Blob, &gone, &f.Scoped, &created, &updated); err != nil {
		return nil, err
	}
	f.GoneAt = parseTS(gone.String)
	f.CreatedAt, f.UpdatedAt = parseTS(created), parseTS(updated)
	return &f, nil
}

// GetFile returns a file, or nil.
func (d *DB) GetFile(id string) (*File, error) {
	f, err := scanFile(d.conn.QueryRow(`SELECT `+fileCols+` FROM vault_files WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

// ListFiles returns the files of one source, or of all if sourceID is "".
func (d *DB) ListFiles(sourceID string) ([]File, error) {
	q, args := `SELECT `+fileCols+` FROM vault_files ORDER BY id`, []any(nil)
	if sourceID != "" {
		q, args = `SELECT `+fileCols+` FROM vault_files WHERE source_id = ? ORDER BY id`, []any{sourceID}
	}
	rows, err := d.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// DeleteFiles removes files with their links and suggestions. It is for the
// explicit prune of tombstoned, unlinked files.
func (d *DB) DeleteFiles(ids []string) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		for _, q := range []string{
			`DELETE FROM vault_file_links WHERE file_id = ?`,
			`DELETE FROM vault_file_suggestions WHERE file_id = ?`,
			`DELETE FROM vault_files WHERE id = ?`,
		} {
			if _, err := tx.Exec(q, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// CreateFileLink inserts a link.
func (d *DB) CreateFileLink(l FileLink) error {
	_, err := d.conn.Exec(
		`INSERT INTO vault_file_links (id, file_id, holder, kind, pratica_slug, origin, tier, blob, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.FileID, l.Holder, l.Kind, l.PraticaSlug, l.Origin, l.Tier, l.Blob, ts(l.CreatedAt))
	return err
}

// ListFileLinks returns every link, or those of one file if fileID is "".
func (d *DB) ListFileLinks(fileID string) ([]FileLink, error) {
	q, args := `SELECT id, file_id, holder, kind, pratica_slug, origin, tier, blob, created_at FROM vault_file_links ORDER BY created_at, id`, []any(nil)
	if fileID != "" {
		q = `SELECT id, file_id, holder, kind, pratica_slug, origin, tier, blob, created_at FROM vault_file_links WHERE file_id = ? ORDER BY created_at, id`
		args = []any{fileID}
	}
	rows, err := d.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileLink
	for rows.Next() {
		var l FileLink
		var created string
		if err := rows.Scan(&l.ID, &l.FileID, &l.Holder, &l.Kind, &l.PraticaSlug, &l.Origin, &l.Tier, &l.Blob, &created); err != nil {
			return nil, err
		}
		l.CreatedAt = parseTS(created)
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetFileLink returns a link, or nil.
func (d *DB) GetFileLink(id string) (*FileLink, error) {
	var l FileLink
	var created string
	err := d.conn.QueryRow(`SELECT id, file_id, holder, kind, pratica_slug, origin, tier, blob, created_at FROM vault_file_links WHERE id = ?`, id).
		Scan(&l.ID, &l.FileID, &l.Holder, &l.Kind, &l.PraticaSlug, &l.Origin, &l.Tier, &l.Blob, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.CreatedAt = parseTS(created)
	return &l, nil
}

// DeleteFileLink removes a link. It reports whether there was one.
func (d *DB) DeleteFileLink(id string) (bool, error) {
	res, err := d.conn.Exec(`DELETE FROM vault_file_links WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CreateFileRule inserts a rule.
func (d *DB) CreateFileRule(r FileRule) error {
	_, err := d.conn.Exec(
		`INSERT INTO vault_file_rules (id, source_id, holder, kind, pratica_slug, blob, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.SourceID, r.Holder, r.Kind, r.PraticaSlug, r.Blob, ts(r.CreatedAt))
	return err
}

// ListFileRules returns the rules of one source, or all if sourceID is "".
func (d *DB) ListFileRules(sourceID string) ([]FileRule, error) {
	q, args := `SELECT id, source_id, holder, kind, pratica_slug, blob, created_at FROM vault_file_rules ORDER BY created_at, id`, []any(nil)
	if sourceID != "" {
		q = `SELECT id, source_id, holder, kind, pratica_slug, blob, created_at FROM vault_file_rules WHERE source_id = ? ORDER BY created_at, id`
		args = []any{sourceID}
	}
	rows, err := d.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileRule
	for rows.Next() {
		var r FileRule
		var created string
		if err := rows.Scan(&r.ID, &r.SourceID, &r.Holder, &r.Kind, &r.PraticaSlug, &r.Blob, &created); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTS(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteFileRule removes a rule. Links it made stay (they are the user's to
// remove); it reports whether there was a rule.
func (d *DB) DeleteFileRule(id string) (bool, error) {
	res, err := d.conn.Exec(`DELETE FROM vault_file_rules WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CreateFileSuggestion inserts a suggestion.
func (d *DB) CreateFileSuggestion(s FileSuggestion) error {
	_, err := d.conn.Exec(
		`INSERT INTO vault_file_suggestions (id, file_id, holder, kind, pratica_slug, proposed_by, blob, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.FileID, s.Holder, s.Kind, s.PraticaSlug, s.ProposedBy, s.Blob, ts(s.CreatedAt))
	return err
}

// ListFileSuggestions returns every pending suggestion, oldest first.
func (d *DB) ListFileSuggestions() ([]FileSuggestion, error) {
	rows, err := d.conn.Query(`SELECT id, file_id, holder, kind, pratica_slug, proposed_by, blob, created_at FROM vault_file_suggestions ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileSuggestion
	for rows.Next() {
		var s FileSuggestion
		var created string
		if err := rows.Scan(&s.ID, &s.FileID, &s.Holder, &s.Kind, &s.PraticaSlug, &s.ProposedBy, &s.Blob, &created); err != nil {
			return nil, err
		}
		s.CreatedAt = parseTS(created)
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteFileSuggestion removes a suggestion (accepted or rejected).
func (d *DB) DeleteFileSuggestion(id string) (bool, error) {
	res, err := d.conn.Exec(`DELETE FROM vault_file_suggestions WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FileHolderCounts returns how many links, rules and suggestions name a
// holder, for the report a person's removal gives.
func (d *DB) FileHolderCounts(holder string) (links, rules, suggestions int, err error) {
	var proposals int
	for _, q := range []struct {
		sql string
		out *int
	}{
		{`SELECT COUNT(*) FROM vault_file_links WHERE holder = ?`, &links},
		{`SELECT COUNT(*) FROM vault_file_rules WHERE holder = ?`, &rules},
		{`SELECT COUNT(*) FROM vault_file_suggestions WHERE holder = ?`, &suggestions},
		{`SELECT COUNT(*) FROM vault_file_proposals WHERE holder = ?`, &proposals},
	} {
		if err = d.conn.QueryRow(q.sql, holder).Scan(q.out); err != nil {
			return 0, 0, 0, err
		}
	}
	return links, rules, suggestions + proposals, nil
}

// FileProposal is a file an agent proposed for the index. Holder is whom it
// asked to link the file to ("" for the owner, or for no one), so scope
// checks need no decryption.
type FileProposal struct {
	ID         string
	Provider   string
	Holder     string
	ProposedBy string
	Blob       string
	CreatedAt  time.Time
}

// CreateFileProposal inserts a proposal.
func (d *DB) CreateFileProposal(p FileProposal) error {
	_, err := d.conn.Exec(
		`INSERT INTO vault_file_proposals (id, provider, holder, proposed_by, blob, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Provider, p.Holder, p.ProposedBy, p.Blob, ts(p.CreatedAt))
	return err
}

// ListFileProposals returns every pending proposal, oldest first.
func (d *DB) ListFileProposals() ([]FileProposal, error) {
	rows, err := d.conn.Query(`SELECT id, provider, holder, proposed_by, blob, created_at FROM vault_file_proposals ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileProposal
	for rows.Next() {
		var p FileProposal
		var created string
		if err := rows.Scan(&p.ID, &p.Provider, &p.Holder, &p.ProposedBy, &p.Blob, &created); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTS(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteFileProposal removes a proposal (accepted or rejected).
func (d *DB) DeleteFileProposal(id string) (bool, error) {
	res, err := d.conn.Exec(`DELETE FROM vault_file_proposals WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
