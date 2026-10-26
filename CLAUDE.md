# Personal Vault

An encrypted local vault for Italian bureaucracy data (a person's, their family's, or a
business's and its people), served to the CLI, an onboarding page and an MCP server.

## Quick Reference

- **Language:** Go 1.26+, no CGO
- **Database:** `modernc.org/sqlite` (pure Go)
- **Crypto:** `golang.org/x/crypto` (argon2, hkdf) + stdlib `crypto/aes`, `crypto/cipher`
- **HTTP:** stdlib `net/http`
- **Tests:** `go test -race ./...` (MCP: `cd mcp && npx tsc -p tsconfig.test.json && npm test`; tsx doesn't check types)
- **Build:** `make build` (outputs `bin/pvault`, and `bin/pvault-touchid` on macOS)

## Commands

```sh
make build                        # Build bin/pvault (+ bin/pvault-touchid on macOS)
make touchid                      # Rebuild only the Touch ID helper
make test                         # Go tests with race detector (not the MCP ones)
make schema                       # Regenerate schema.json after a schema change (a test checks)
make clean                        # Remove build artifacts
go test -race ./internal/vault/   # One layer: crypto, store, vault, api
```

## CLI Usage

```sh
pvault init [--impresa]                  # Create new vault (~/.pvault/); --impresa: a business vault
pvault --vault <name> <command>          # Any command on another vault (~/.pvault-<name>)
pvault unlock | lock | serve             # Background server / stop it, zero keys / foreground
pvault set <category.field> <value>      # Set encrypted field
pvault get <category.field|category.*> [...]  # Get decrypted field(s); several → JSON, one grant
pvault list [category]                   # List fields
pvault delete <category.field>           # Delete field
pvault set-sensitivity <id> <tier>       # Set sensitivity tier
pvault export | audit | status           # All fields as JSON / access log / status
pvault backup [--to <folder>]            # Encrypted backup now; --to sets the auto-backup folder
pvault restore <file> [--check] [--ask-key]  # Recreate vault from a backup / only verify it
pvault expiring [days]                   # Expired/expiring *data_scadenza fields and pratiche
pvault doc add|list|get|delete           # Encrypted document scans
pvault person list|show|add|remove|export|migrate  # Other people (leo:cie.numero); alias: member
pvault retention [set <years>|unset]     # How long people's data is kept after ruolo.data_fine (no default)
pvault retention scans <days>|unset      # How long clients' ID scans are kept after their pratiche close (no default)
pvault touchid enable|disable|status     # Touch ID approvals (macOS)
pvault create-service-token|list-service-tokens|revoke-service-token
pvault ui                                # Onboarding page in the default browser
```

`pvault help` prints every command with its flags.

## Architecture

```
cmd/pvault/          CLI (thin HTTP clients to the vault server)
cmd/pvault-touchid/  Swift helper: Touch ID approval, then a grant over the vault socket
internal/
  crypto/            KDF (Argon2id), cipher (AES-256-GCM), HKDF subkeys, backup stream
  store/             SQLite CRUD (fields, documents, people, tokens, audit, meta)
  vault/             Business logic: rules, encrypt/decrypt, sessions, people, backups
  api/               HTTP server, handlers, auth middleware, onboarding page (ui/)
mcp/                 MCP server (TypeScript): vault tools over a service token, read-only PEC
claude/skills/       vault and pratica skills; pratica keeps one playbook per procedure
```

## Security Model

```
Profile Password + Secret Key (128-bit)
  → Argon2id (64MB, 3 iter) → Vault Key (256-bit, in-memory only)
  → HKDF per category → Category Subkeys (documents: info documents/<category>)
  → AES-256-GCM per field (12-byte random nonce)
```

### Keys and sessions
- Encrypt before INSERT, decrypt after SELECT. The vault key lives only in memory while
  unlocked: `requireUnlocked` hands out a copy each caller clears (`defer clear(...)`, subkeys
  too); callers that don't need it use `checkUnlocked`. Auto-lock after 30 min idle.
- `secret.key` (0600) is never in the database or a backup. `Init` never replaces one and
  removes a half-made vault on failure; `Open` never creates `vault.db`.
- Session tokens: 32 random bytes, constant-time compare, never on disk, never in a URL.
- Each CLI command re-enters the password (TTY only) and trades it at `POST /vault/session`
  for an in-memory grant (2m CLI, 15m UI, max 1h). Only wrong passwords count toward the
  10/min limit (5/min for `/vault/unlock`); attempts are counted on entry and refunded
  otherwise.
- Credential endpoints require `Content-Type: application/json`; every request needs a
  loopback Host (DNS rebinding).

### Credentials never travel over TCP
- `pvault serve` listens on `<vault dir>/run/pvault.sock` (dir 0700) before TCP. The CLI sends
  passwords only there (`credentialRequest`, `socket.go`), after `checkVaultServer` verifies
  the listener is this same pvault build (peer uid, exe path, cdhash; `peer_darwin.go`,
  Linux `SO_PEERCRED`) and that `VAULT_ADDR` is the address that server reports. It runs
  before every password or Touch ID prompt. The onboarding page is the one exception (a
  browser can't use a socket). The page is self-contained under a hash-pinned CSP
  (`pageCSP`), and never shows a secret again: the page asks for category reads with `?omit=` the
  password inputs, which come back `redacted` and read "saved".
- Limits: this catches stale or impostor servers, not a process that can already write to
  the vault dir or replace the pvault binary.

### Touch ID (`cmd/pvault-touchid/main.swift`)
- The profile password sits in a login-Keychain item whose ACL trusts only that helper
  build; the helper checks Touch ID itself, sends the password only over the socket to the
  enrolled pvault (audit token → valid hardened code with the enrolled cdhash), and returns
  only a token. Rebuilding the helper or moving pvault means `pvault touchid enable` again.
- The prompt is worded from the exact scope it requests and never understates it.
  Patterns are anchored with `\A`/`\z`. Its parser is held to `internal/vault/testdata/scopes.json`.
- `touchid enable`/`disable` refuse without a terminal, so a script can't switch them; an
  agent with a pty could, which only falls back to the password.
- CLI commands call `needGrant(scope, access)`: with Touch ID the grant is scoped to that
  (full access if unset). A scoped grant refused with 403 `session_required` makes `authed`
  ask once more for full access.

### Who may do what
- **Session** (the user, typed password): everything. Handlers test `isSessionAuth`.
- **Scoped grant** (`POST /vault/session` with `scope`, max 10m): what the user approved
  in person, `personal` tier included. Not session auth.
- **Service token** (agents, MCP): explicit scope, access `read`/`append`/`write`, TTL
  24h default / 90d max, `max_sensitivity` cap (default `sensitive`, never `personal`).
  Never resets the auto-lock timer. A token may name only people who exist.
- Tiers: `public` < `standard` < `sensitive` < `critical` < `personal`. Service tokens never
  see `personal` items (404 on read/delete, 403 on write, left out of listings).
- Only the session may lower a tier, or delete a field or document stored above its
  default tier. A write without a tier keeps the stored one in the same SQL statement
  (`KeepTier`). A token's write or delete lands only if the field is still as it checked
  it (`store.Seen`, else 409).
- Reads trust the stored tier, so `Open` raises to `personal` anything the current rules
  call a credential (`raiseCredentialTiers`).

### Backups (`backup.go`)
- A `vault.db` snapshot plus document files in a tar, encrypted with chunked AES-GCM under
  a per-file HKDF key; the plaintext header is the AAD. Never `secret.key`, never service
  tokens (a restore must not revive a revoked one). Session-only.
- Every write calls `markChanged` (backup 1m after the last change); lock and auto-lock
  flush first. Pruning touches only this vault's files.
- Lock order: `backup.mu` → `writeMu` → `v.mu` → session `mu`. `backup.stateMu` is a leaf.
  Every single write holds `writeMu` for reading; removing or migrating people holds it
  exclusively.

## Data Rules

All value rules run in `Vault.Set`: `checkKind` first (what this kind of vault can hold),
then the pure `NormalizeAndValidate`. `Vault.CanHold` answers the same checks for a field
ID without a value; the onboarding page shows only what it allows (`GET /ui/fields`), so
the page never copies a rule.

- **IDs** are `category.field_name`, or `<slug>:category.field_name` for another person;
  lower-case only (`CheckLowerCase`; Unlock migrates older fields, `migrateLegacyFields`).
  Rules read stored IDs as they are, never ignoring case.
- **Numbered copies** of a schema category (`cie2`, `abitazione-2`) follow every rule of the
  original: rules read `canonicalLocalID`/`schemaCategoryOf`. `famiglia`, `ruolo` and
  `pratiche` (one per holder) have none (`oneOnly`).
- **Credentials** (`IsCredentialField`: a secret word like pin/puk/password/otp in the
  category or field name, wider words next to spid/cie/firma) are stored only as
  `personal`; SPID/CieID categories and their documents default to it.
- **Formats**: Italian codes, dates (`YYYY-MM-DD`), euro amounts and areas are validated
  and normalized (`italy.go`, `dates.go`, `housing.go`, `business.go`). Housing categories
  (`abitazione`, `locazione`, `utenze`, `tari`) describe the home at residenza/domicilio.
  New fields the pratiche-napoli library needs are added here, then dropped from its
  `contract/extensions.json`. `schema.json` is `pvault schema --json`, credential words
  included (`Credentials`, the expressions `IsCredentialField` compiles); the library's CI
  compares its copy with the one on main.
- **Pratiche** (`pratiche.go`): `pratiche.<slug>_<campo>`, campo/stato/canale from fixed
  lists, never credentials. A deadline shows in `Expiring` until the stato closes. The
  store dates each closing (`closed_at`, kept across corrections between closed stati).
  `praticheIn` is the one reading of "is this pratica closed, and for whom".
- **Documents** (`documents.go`): AAD bound to the document ID, encrypted filename,
  `documents/<id>.enc` (0600); token scope ID `<category>.documents`. Downloads are
  attachments with `Content-Security-Policy: sandbox`.

### People (`scope.go`, `people.go`, `household.go`)
- A person is a row in `vault_people`: added by their first field or document (a SQLite
  trigger), removed only by `RemovePerson`. Deleting their last row leaves them, and
  `pvault person add` refuses a slug the vault has (a new person never inherits what
  named an earlier one). Slugs are
  `^[a-z][a-z0-9-]{0,31}$`; `io` is the owner, who has no prefix.
- The stored category (`leo:cie`) is the HKDF info, so each person has their own keys.
  Every category rule strips the prefix first (`SplitPerson`), so `leo:spid.password` is
  personal like `spid.password` (`TestPersonRulesMatchOwner`).
- The schema says which fields are owner-only or person-only (`For`) and which name people
  (`PersonRef`, plus pratiche `persona`); housing and `pratiche` are owner-only, `procura`
  person-only. A value naming people must name `io` or existing people; a service token
  only people it can see a row of.
- Scopes: unprefixed patterns reach only the owner; `leo:cat.*`, `leo:cat.field`, `leo:*`;
  `*:cat.*` for everyone; `*` everything; `*:*` and `io:` refused.
- `RemovePerson` (the session, or a grant the user approved for `<slug>:*` write) refuses
  while another field names the person (a closed pratica doesn't count), and deletes their
  rows, the person and the tokens naming them in one transaction; then drops grants and
  files (`files_left` if one stays).
- Service tokens never learn of people they can't see a row of (the household view drops
  such slugs; a field in scope reads as written) and never create one (`checkPersonExists`:
  a write under `*:` is refused for a slug that doesn't exist, so it can tell whether a
  slug exists, only within what it may write); public status counts only the owner's data.
- A closed pratica may keep naming a removed person; reopening it, by writing its stato
  or deleting it, checks `persona` again (`checkPraticaPersona`, whatever the caller reads).
- Old JSON and audit names stay for compatibility: `/vault/members`, `member`, `members`,
  `"for": "member"`, `member_remove`, MCP `vault_household`.

### Vault kinds and named vaults (`kind.go`, `cmd/pvault/vaults.go`)
- Meta `kind`: `persona` (default) or `impresa`, fixed at init. Schema categories carry
  `subject` and `kinds`. `checkKind` refuses the other kind's categories (`famiglia`,
  `ruolo`), persona categories as the business's own data, housing in a business vault
  (its premises are `locale`), and in a business vault any person's SPID/CieID or login
  secret; the business itself may hold only its own SPID and portal logins.
- `--vault <name>` (first argument) or `PVAULT_VAULT` sets `VAULT_DIR=~/.pvault-<name>`
  before dispatch. Each vault's TCP port is in `<dir>/port` (7200 for `~/.pvault`, else the
  lowest free 7201-7299); it is never trusted (`checkVaultServer`). Hints use `pvCmd` so
  they name `--vault` (the Swift helper has its own `pvCmd`).

### Clients and retention (`procura.go`, `retention.go`, `export.go`, `expiry.go`)
- Retention periods have no default (D8): meta `retention_years`, and for business vaults
  `scan_days`, set only by the session. `Expiring` then reports `kind: retention` (a
  person's `ruolo.data_fine` + years) and `kind: pratiche_closed` (a client's ID and
  procura scans, once every pratica naming them closed, dated last closing + days).
- A scan is reported only if it predates the last closing (a same-second one counts as
  after), and a procura's scans only once its `data_scadenza` has passed (a procura with
  none is still in force); a caller that can't see everything relevant gets no entry. Nothing is ever deleted automatically.
- `GET /vault/members/{slug}/export` is session-only and audited; personal-tier items only
  with `include_personal=1`. `pvault person export` writes a new 0700 folder, files 0600.

### MCP (`mcp/src`)
- Uses only a service token (`VAULT_TOKEN`, else `VAULT_KEYCHAIN_SERVICE`, else
  `VAULT_TOKEN_COMMAND`), looked up per request; the server enforces its scope. It refuses
  to start with the old `VAULT_SCOPE` set.
- PEC (`pec.ts`): read-only IMAP (EXAMINE + BODY.PEEK), registered only with
  `PEC_ADDRESS`, app password from Keychain per request, no SMTP code by design. Saved
  attachments go to the first import dir (0600, quarantined). Mailparser needs
  `ignoreEmbedded: true` or it splices `postacert.eml` into the envelope.

## Conventions

- All timestamps are RFC3339 strings in SQLite, to the second.
- WAL mode, busy_timeout=5000ms; the vault path can't contain `?`.
- CLI arguments: unknown options and repeated flags are errors, never ignored
  (`positionalArgs`, `parseFlags`); `pvault set` takes one value.
- A comment says why; CLAUDE.md says what must stay true. Keep both short.

## Environment Variables

- `VAULT_DIR` — vault directory (default: `~/.pvault`)
- `PVAULT_VAULT` — like `--vault <name>`: the vault in `~/.pvault-<name>`
- `VAULT_ADDR` — server address for the CLI (default: from the vault's `port` file, else `http://127.0.0.1:7200`)
- `VAULT_PORT` — server port for `pvault serve` (default: the vault's `port` file, else `7200`)

## Testing

Tests use temp directories and run with `-race`. `internal/vault` and `internal/api` call
`crypto.UseFastKDFForTests` in `TestMain` (cheap Argon2id; it panics outside a test binary).
API requests go through `newTestRequest` (loopback Host); socket tests use
`serveOnSocket`/`shortVaultDir` (temp paths exceed the 104-byte socket limit). The Touch ID
tests compile the helper with `-D DEBUG`, which adds `describe-grant` and test enrollment
(`PVAULT_TOUCHID_TEST_*`). Keychain tests run only with `PVAULT_KEYCHAIN_TEST=1`.

```go
// Pattern: create temp vault, run test
func tmpVault(t *testing.T) (*Vault, string) {
    dir := filepath.Join(t.TempDir(), ".pvault")
    sk, _ := Init(dir, "test-password")
    v, _ := Open(dir)
    v.Unlock("test-password", sk)
    return v, sk
}
```
