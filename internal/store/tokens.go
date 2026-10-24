package store

import (
	"database/sql"
	"time"
)

// Token represents a session token.
type Token struct {
	TokenStr       string
	Consumer       string
	Scope          string
	ExpiresAt      time.Time
	Usage          string
	CreatedAt      time.Time
	MaxSensitivity string // highest sensitivity tier the token may read
	Access         string // read, append or write
}

// CreateToken inserts a new session token.
func (d *DB) CreateToken(t Token) error {
	_, err := d.conn.Exec(
		`INSERT INTO vault_tokens (token, consumer, scope, expires_at, usage, created_at, max_sensitivity, access)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TokenStr, t.Consumer, t.Scope, t.ExpiresAt.UTC().Format(time.RFC3339),
		t.Usage, t.CreatedAt.UTC().Format(time.RFC3339), maxSensitivityOrDefault(t.MaxSensitivity), accessOrDefault(t.Access),
	)
	return err
}

// GetToken retrieves a token if it exists and hasn't expired.
func (d *DB) GetToken(token string) (*Token, error) {
	var t Token
	var expiresAt, createdAt string
	err := d.conn.QueryRow(
		"SELECT token, consumer, scope, expires_at, usage, created_at, max_sensitivity, access FROM vault_tokens WHERE token = ?",
		token,
	).Scan(&t.TokenStr, &t.Consumer, &t.Scope, &expiresAt, &t.Usage, &createdAt, &t.MaxSensitivity, &t.Access)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	t.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	if time.Now().After(t.ExpiresAt) {
		return nil, nil
	}
	return &t, nil
}

// DeleteToken removes a token. Returns the number of rows deleted.
func (d *DB) DeleteToken(token string) (int64, error) {
	result, err := d.conn.Exec("DELETE FROM vault_tokens WHERE token = ?", token)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteExpiredTokens removes expired tokens.
func (d *DB) DeleteExpiredTokens() (int64, error) {
	result, err := d.conn.Exec("DELETE FROM vault_tokens WHERE expires_at < ?", time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteAllTokens removes all tokens.
func (d *DB) DeleteAllTokens() (int64, error) {
	result, err := d.conn.Exec("DELETE FROM vault_tokens")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ListTokensByUsage returns tokens with the given usage type.
func (d *DB) ListTokensByUsage(usage string) ([]Token, error) {
	rows, err := d.conn.Query(
		"SELECT token, consumer, scope, expires_at, usage, created_at, max_sensitivity, access FROM vault_tokens WHERE usage = ? ORDER BY created_at DESC",
		usage,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		var expiresAt, createdAt string
		if err := rows.Scan(&t.TokenStr, &t.Consumer, &t.Scope, &expiresAt, &t.Usage, &createdAt, &t.MaxSensitivity, &t.Access); err != nil {
			return nil, err
		}
		t.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// maxSensitivityOrDefault returns the restrictive default for tokens created
// without an explicit tier.
func maxSensitivityOrDefault(tier string) string {
	if tier == "" {
		return "sensitive"
	}
	return tier
}

// accessOrDefault returns read-only access for tokens created without one.
func accessOrDefault(access string) string {
	if access == "" {
		return "read"
	}
	return access
}
