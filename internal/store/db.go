package store

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// legacyTokenMaxTTL caps tokens created before max_sensitivity existed
// (which defaulted to full scope for a year) at the current 90-day maximum.
const legacyTokenMaxTTL = 90 * 24 * time.Hour

const createSchema = `
CREATE TABLE IF NOT EXISTS vault_fields (
	id          TEXT PRIMARY KEY,
	category    TEXT NOT NULL,
	field_name  TEXT NOT NULL,
	value       TEXT NOT NULL,
	sensitivity TEXT NOT NULL DEFAULT 'standard',
	updated_at  TEXT NOT NULL,
	version     INTEGER NOT NULL DEFAULT 1,
	closed_at   TEXT
);

CREATE TABLE IF NOT EXISTS vault_access_log (
	id         TEXT PRIMARY KEY,
	consumer   TEXT NOT NULL,
	scope      TEXT NOT NULL,
	action     TEXT NOT NULL,
	purpose    TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS vault_tokens (
	token      TEXT PRIMARY KEY,
	consumer   TEXT NOT NULL,
	scope      TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	usage      TEXT NOT NULL DEFAULT 'multi',
	created_at TEXT NOT NULL,
	max_sensitivity TEXT NOT NULL DEFAULT 'sensitive',
	access     TEXT NOT NULL DEFAULT 'read'
);

CREATE TABLE IF NOT EXISTS vault_documents (
	id           TEXT PRIMARY KEY,
	category     TEXT NOT NULL,
	name         TEXT NOT NULL,
	content_type TEXT NOT NULL,
	size         INTEGER NOT NULL,
	sensitivity  TEXT NOT NULL DEFAULT 'sensitive',
	created_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS vault_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

-- The vault's other people (people.go). Writing a person's first field or
-- document adds them; only ApplyChange's DeletePerson removes one.
CREATE TABLE IF NOT EXISTS vault_people (
	slug       TEXT PRIMARY KEY,
	created_at TEXT NOT NULL
);

CREATE TRIGGER IF NOT EXISTS vault_fields_person AFTER INSERT ON vault_fields
WHEN instr(NEW.category, ':') > 0 BEGIN
	INSERT OR IGNORE INTO vault_people (slug, created_at)
	VALUES (substr(NEW.category, 1, instr(NEW.category, ':') - 1), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
END;

CREATE TRIGGER IF NOT EXISTS vault_documents_person AFTER INSERT ON vault_documents
WHEN instr(NEW.category, ':') > 0 BEGIN
	INSERT OR IGNORE INTO vault_people (slug, created_at)
	VALUES (substr(NEW.category, 1, instr(NEW.category, ':') - 1), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
END;

CREATE INDEX IF NOT EXISTS idx_fields_category ON vault_fields(category);
CREATE INDEX IF NOT EXISTS idx_documents_category ON vault_documents(category);
CREATE INDEX IF NOT EXISTS idx_fields_sensitivity ON vault_fields(sensitivity);
CREATE INDEX IF NOT EXISTS idx_tokens_expires ON vault_tokens(expires_at);
CREATE INDEX IF NOT EXISTS idx_access_log_created ON vault_access_log(created_at);
`

// DB wraps a *sql.DB with vault-specific operations.
type DB struct {
	conn *sql.DB
}

// Open opens or creates the vault database at the given path.
func Open(path string) (*DB, error) {
	if err := checkPath(path); err != nil {
		return nil, err
	}
	// Owner-only, also for databases created before this check. SQLite gives
	// the -wal and -shm files it creates the database file's mode.
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600); err == nil {
		f.Close()
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Chmod(path+suffix, 0600)
	}

	// The settings go in the DSN so the driver applies them to every pooled
	// connection; a PRAGMA run once reaches only one of them, and the others
	// would fail with SQLITE_BUSY instead of waiting for a lock.
	conn, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := conn.Exec(createSchema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrating schema: %w", err)
	}

	return &DB{conn: conn}, nil
}

// migrate upgrades databases created by older versions in place.
func migrate(conn *sql.DB) error {
	if err := ensureColumn(conn, "vault_tokens", "max_sensitivity", addTokenMaxSensitivity); err != nil {
		return err
	}
	if err := ensureColumn(conn, "vault_tokens", "access", addTokenAccess); err != nil {
		return err
	}
	if err := ensureColumn(conn, "vault_fields", "closed_at", addFieldClosedAt); err != nil {
		return err
	}
	return addPeopleWithRows(conn)
}

// addPeopleWithRows adds to vault_people everyone with a field or document,
// for databases from before the table (and restored backups of them).
func addPeopleWithRows(conn *sql.DB) error {
	_, err := conn.Exec(`INSERT OR IGNORE INTO vault_people (slug, created_at)
		SELECT substr(category, 1, instr(category, ':') - 1), strftime('%Y-%m-%dT%H:%M:%SZ', 'now') FROM vault_fields WHERE instr(category, ':') > 0
		UNION
		SELECT substr(category, 1, instr(category, ':') - 1), strftime('%Y-%m-%dT%H:%M:%SZ', 'now') FROM vault_documents WHERE instr(category, ':') > 0`)
	return err
}

// addFieldClosedAt adds when a pratica closed (Field.ClosedAt). The vault
// dates older closed stati when it unlocks, since only it can read them.
func addFieldClosedAt(conn *sql.DB) error {
	_, err := conn.Exec(`ALTER TABLE vault_fields ADD COLUMN closed_at TEXT`)
	return err
}

// ensureColumn runs add if table lacks column, tolerating another process
// adding it between the check and the ALTER.
func ensureColumn(conn *sql.DB, table, column string, add func(*sql.DB) error) error {
	has, err := hasColumn(conn, table, column)
	if err != nil || has {
		return err
	}
	if err := add(conn); err != nil {
		if has, herr := hasColumn(conn, table, column); herr == nil && has {
			return nil
		}
		return err
	}
	return nil
}

// addTokenAccess adds the access column. Existing tokens become read-only:
// write access must now be granted explicitly.
func addTokenAccess(conn *sql.DB) error {
	_, err := conn.Exec(`ALTER TABLE vault_tokens ADD COLUMN access TEXT NOT NULL DEFAULT 'read'`)
	return err
}

// addTokenMaxSensitivity adds the max_sensitivity column and caps legacy
// token lifetimes in one transaction, so a crash cannot leave one without
// the other. Existing tokens get the restrictive default: no critical fields.
func addTokenMaxSensitivity(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`ALTER TABLE vault_tokens ADD COLUMN max_sensitivity TEXT NOT NULL DEFAULT 'sensitive'`); err != nil {
		return err
	}
	maxExpiry := time.Now().Add(legacyTokenMaxTTL).UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE vault_tokens SET expires_at = ? WHERE expires_at > ?`, maxExpiry, maxExpiry); err != nil {
		return err
	}
	return tx.Commit()
}

func hasColumn(conn *sql.DB, table, column string) (bool, error) {
	rows, err := conn.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// checkPath refuses paths the SQLite driver would misread: it cuts its
// connection string at the first '?', so a vault folder named "a?b" would
// put the database in a file named "a" outside it.
func checkPath(path string) error {
	if strings.Contains(path, "?") {
		return fmt.Errorf("the vault path %q can't contain '?'", path)
	}
	return nil
}
