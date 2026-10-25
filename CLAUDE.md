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
make build                        # Build bin/pvault (+ bin/pvault-touchid on macOS)
make touchid                      # Rebuild only the Touch ID helper
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
pvault get <category.field> [...]        # Get decrypted field(s); several → JSON, one grant
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
- Touch ID (`cmd/pvault-touchid/main.swift`, macOS, ad-hoc signed + hardened runtime):
  `pvault touchid enable` (TTY only) stores the profile password in a plain login-Keychain
  item (service `pvault-touchid`, account = vault dir) whose ACL trusts only that exact
  helper build. Biometric Keychain flags need a paid Apple signature (-34018 otherwise),
  so the helper checks Touch ID itself (biometrics or Apple Watch, no Mac-password
  fallback), then talks to the server at the fixed address 127.0.0.1:7200 and returns
  only a token, never the password. Its prompt is worded from the exact scope it requests:
  append/write grants say they can read too, `cat.*` says "fields and documents", `*` is
  named alone, durations round up, and patterns are anchored with `\A`/`\z` (ICU `$` lets
  a trailing line break through) and capped at 100 characters.
  pvault looks for the helper only next to its own binary. CLI commands call
  `needGrant(scope, access)`; with Touch ID on, the grant is scoped to that (full access
  if unset), and in a TTY a failed or denied prompt falls back to the typed password.
  `pvault unlock` with Touch ID starts `pvault serve --locked` and the helper unlocks it.
  Rebuilding the helper changes its signature, so re-run `pvault touchid enable`.
  pvault skips Touch ID when its `VAULT_ADDR` differs from the helper's server (`status`
  prints `server <addr>`), and a `unlock` that started the server stops it again if no one
  can unlock it. `touchid enable`/`disable` are interactive only.
  Debug builds (`-D DEBUG`) honor `VAULT_ADDR` and `PVAULT_TOUCHID_TEST_APPROVE=1` for tests,
  and add `describe-grant` (prints the prompt; `TestTouchIDHelper_*` compiles it with swiftc)
- Scoped grants: `POST /vault/session` with `scope` (+ `access`) issues a grant limited
  like a service token (TTL max 10m) that can reach `personal` fields, because the user
  approved it in person. It is not session auth (no audit, lock, tiers or tokens), and only
  its creation resets the auto-lock timer.
- Only session auth may lower a tier: a token or scoped grant writing a field with a
  `sensitivity` below its current tier (or the schema default) gets 403.
  Handlers must test `isSessionAuth`, not `maxTier == personal`, to mean "the user's session"
- Service tokens: access `read` (default) / `append` (add new fields and
  documents, no overwrite or delete) / `write`; explicit scope required, TTL 24h default / 90d max,
  `max_sensitivity` cap (default `sensitive`), cannot change tiers,
  and never reset the auto-lock timer (only session activity does)
- MCP server uses only a service token: `VAULT_TOKEN`, else `VAULT_KEYCHAIN_SERVICE`
  (Keychain item for the current user), else `VAULT_TOKEN_COMMAND`; lookups run per
  request, never cached. `create-service-token --keychain <service>` stores the token
  via `/usr/bin/security -i` stdin instead of printing it and revokes the token it
  replaces (by hash, so no raw token goes in a URL). `TestStoreInKeychain` and the
  MCP Keychain test touch the real Keychain and only run with `PVAULT_KEYCHAIN_TEST=1`
- PEC (`mcp/src/pec.ts`): read-only IMAP tools `pec_folders`/`pec_list`/`pec_read`/
  `pec_attachment`/`pec_save_attachment` (writes into the first import dir, `wx` + 0600), registered only when `PEC_ADDRESS` is set. No SMTP code by design.
  EXAMINE + BODY.PEEK so nothing is marked read. The app password comes from Keychain
  (`PEC_KEYCHAIN_SERVICE`, account = the PEC address) per request, never cached.
  Messages are unwrapped from `postacert.eml` and `daticert.xml` is parsed
  (mailparser needs `ignoreEmbedded: true` or it splices the inner message into the envelope)
- Italian fields are validated/normalized in `Vault.Set` (`internal/vault/italy.go`);
  SPID/CIE credentials (`IsCredentialField`) can only be stored with tier
  `personal`; `spid`/`cieid` categories default to it. `personal` (rank 4) is
  session-only: service tokens can't be capped at it, and the API hides
  personal items (404 on read/delete, 403 personal_only on write, omitted
  from every listing). Portal login: `preferences.accesso_pa` (`spid`/`cie`) and
  `spid.provider` (fixed spellings matching the onboarding select,
  `TestOnboardingSelectOptionsValidate`) are validated in `access.go`; the provider
  stays lowerable to `standard` so agents can pick it. Fields named with the word
  shape `data_<x>`, `date_of_<x>` or `<x>_date` must be real dates, stored as
  YYYY-MM-DD (`dates.go`)
- Housing categories `abitazione`, `locazione`, `utenze`, `tari` describe the home at
  residenza/domicilio (address not duplicated). Their format rules live in
  `internal/vault/housing.go` and run before the generic ones in `NormalizeAndValidate`,
  scoped to those categories. Every onboarding `data-field` category must be in the
  page's `categories` list (`TestOnboardingFieldsAreLoaded`)
- Pratiche (`pratiche.go`): procedures in progress are fields `pratiche.<slug>_<campo>`,
  slug lower-case words joined by hyphens, campo/stato/canale from fixed lists (validated).
  Well-formed pratiche fields are never credentials, even when the slug says cie-pin.
  `<slug>_data_scadenza` shows in `Expiring` (as "pratica <slug>") until the pratica's
  stato is completata/respinta/annullata. The pratica skill (`claude/skills/pratica/`) keeps
  one playbook per procedure in `playbooks/`, with field IDs only, never personal values
- Documents (`documents.go`): AES-256-GCM with HKDF info `documents/<category>`,
  AAD bound to the document ID; encrypted filename; files at
  `~/.pvault/documents/<id>.enc` (0600). IDs must match `^[0-9a-f]{32}$`.
  Token access uses scope ID `<category>.documents` plus the tier cap. Only session auth
  may upload below `DefaultDocumentSensitivity` (`sensitive`); others get 403 session_required.
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
