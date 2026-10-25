# Personal Vault

## Quick Reference

- **Language:** Go 1.26+
- **Database:** `modernc.org/sqlite` (pure Go, no CGO)
- **Crypto:** `golang.org/x/crypto` (argon2, hkdf) + stdlib `crypto/aes`, `crypto/cipher`
- **HTTP:** stdlib `net/http`
- **Tests:** `go test -race ./...`
- **Build:** `make build` (outputs `bin/pvault`)

## Commands

```sh
make build                        # Build binary to bin/pvault
make test                         # Run all tests with race detector
make clean                        # Remove build artifacts
go test -race ./internal/crypto/  # Test crypto layer only
go test -race ./internal/store/   # Test store layer only
go test -race ./internal/vault/   # Test vault core only
go test -race ./internal/api/     # Test HTTP API only
```

## CLI Usage

```sh
pvault init                              # Create new vault (~/.pvault/)
pvault unlock                            # Start background server
pvault lock                              # Stop server, zero keys
pvault serve                             # Foreground server (debugging)
pvault set <category.field> <value>      # Set encrypted field
pvault get <category.field>              # Get decrypted field
pvault list [category]                   # List fields
pvault delete <category.field>           # Delete field
pvault set-sensitivity <id> <tier>       # Set sensitivity tier
pvault export                            # Export all fields as JSON
pvault audit                             # Show access audit log
pvault status                            # Show vault status
```

## Architecture

```
cmd/pvault/      CLI (thin HTTP clients to the vault server)
internal/
  crypto/        KDF (Argon2id), cipher (AES-256-GCM), HKDF subkeys
  store/         SQLite CRUD (fields, documents, tokens, audit, meta)
  vault/         Business logic (init, unlock/lock, encrypt/decrypt, session)
  api/           HTTP server, handlers, Bearer token middleware
```

## Security Model

```
Profile Password + Secret Key (128-bit)
  → Argon2id (64MB, 3 iter) → Vault Key (256-bit, in-memory only)
  → HKDF per category → Category Subkeys
  → AES-256-GCM per field (12-byte random nonce)
```

- App-layer encryption: encrypt before INSERT, decrypt after SELECT
- Vault key exists only in memory while unlocked
- Auto-lock after 30 min idle
- Session token: 32 bytes crypto/rand, constant-time comparison; never written to disk
- CLI auth: each command re-enters the password (interactive TTY only) and
  exchanges it via `POST /vault/session` for an in-memory grant (2m CLI,
  15m UI, max 1h), cleared on lock. Only failed attempts count toward the
  10/min limit. The secret key is held in memory while unlocked, so the
  onboarding page asks for the password alone; no token is ever put in a URL.
- Credential endpoints (`/vault/unlock`, `/vault/session`) require
  `Content-Type: application/json` so web pages can't call them cross-origin,
  and every request must carry a loopback Host (127.0.0.1/localhost/::1) to
  block DNS rebinding. Grant attempts are counted on entry and refunded unless
  the password was wrong, so concurrent guesses can't bypass the limit.
  API tests build requests with `newTestRequest` (loopback Host)
- Writing a field without a tier keeps its existing tier (schema default for
  new fields), so overwrites never silently downgrade sensitivity
- Secret key at `~/.pvault/secret.key` (0600), never in database
- Service tokens: access `read` (default) / `append` (add new fields and
  documents, no overwrite or delete) / `write`; explicit scope required, TTL 24h default / 90d max,
  `max_sensitivity` cap (default `sensitive`), cannot change tiers,
  and never reset the auto-lock timer (only session activity does)
- MCP server uses only a service token: `VAULT_TOKEN`, or `VAULT_TOKEN_COMMAND`
  (run per request, never cached). `create-service-token --keychain <service>` stores the
  token in the macOS Keychain via `security -i` stdin instead of printing it
- Italian fields are validated/normalized in `Vault.Set` (`internal/vault/italy.go`);
  SPID/CIE credentials (`IsCredentialField`) can only be stored with tier
  `personal`; `spid`/`cieid` categories default to it. `personal` (rank 4) is
  session-only: service tokens can't be capped at it, and the API hides
  personal items (404 on read/delete, 403 personal_only on write, omitted
  from every listing). Fields named with the word
  shape `data_<x>`, `date_of_<x>` or `<x>_date` must be real dates, stored as
  YYYY-MM-DD (`dates.go`)
- Documents (`documents.go`): AES-256-GCM with HKDF info `documents/<category>`,
  AAD bound to the document ID; encrypted filename; files at
  `~/.pvault/documents/<id>.enc` (0600). IDs must match `^[0-9a-f]{32}$`.
  Token access uses scope ID `<category>.documents` plus the tier cap.
  Downloads are served as attachments with `Content-Security-Policy: sandbox`

## Conventions

- Field IDs are `category.field_name` (e.g., `identity.full_name`)
- Sensitivity tiers: `public`, `standard`, `sensitive`, `critical`
- All timestamps stored as RFC3339 strings in SQLite
- WAL mode enabled, busy_timeout=5000ms
- No CGO — pure Go for portability

## Environment Variables

- `VAULT_DIR` — vault directory (default: `~/.pvault`)
- `VAULT_ADDR` — server address for CLI (default: `http://127.0.0.1:7200`)
- `VAULT_PORT` — server port for `pvault serve` (default: `7200`)

## Testing

Tests use temp directories — no cleanup needed. All tests run with `-race`.

```go
// Pattern: create temp vault, run test
func tmpVault(t *testing.T) (*Vault, string) {
    dir := filepath.Join(t.TempDir(), ".vault")
    sk, _ := Init(dir, "test-password")
    v, _ := Open(dir)
    v.Unlock("test-password", sk)
    return v, sk
}
```
