package vault

import "time"

// VaultStatus describes the current state of the vault.
type VaultStatus struct {
	Initialized bool           `json:"initialized"`
	Locked      bool           `json:"locked"`
	Kind        string         `json:"kind"` // KindPersona or KindImpresa
	FieldCount  int            `json:"field_count"`
	Documents   int            `json:"documents"`
	Categories  map[string]int `json:"categories"`
	Backup      *BackupStatus  `json:"backup,omitempty"`
}

// FieldInfo is a decrypted field returned to callers.
type FieldInfo struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"`
	FieldName   string    `json:"field_name"`
	Value       string    `json:"value,omitempty"`
	Redacted    bool      `json:"redacted,omitempty"` // the caller asked for the value to be left out (?omit=)
	Derived     bool      `json:"derived,omitempty"`  // computed by the vault from an address (located.go), not stored
	Sensitivity string    `json:"sensitivity"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int       `json:"version"`
	ClosedAt    time.Time `json:"-"` // a pratica's stato: when it closed (store.Field.ClosedAt)
}

// ContextBundle is a full decrypted dump grouped by category.
type ContextBundle struct {
	Categories map[string][]FieldInfo `json:"categories"`
}
