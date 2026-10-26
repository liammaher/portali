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
pvault init [--impresa]                  # Create new vault (~/.pvault/); --impresa: a business vault
pvault --vault <name> <command>          # Any command on another vault (~/.pvault-<name>)
pvault unlock                            # Start background server
pvault lock                              # Stop server, zero keys
pvault serve                             # Foreground server (debugging)
pvault set <category.field> <value>      # Set encrypted field
pvault get <category.field> [...]        # Get decrypted field(s); several → JSON, one grant
pvault list [category]                   # List fields
pvault delete <category.field>           # Delete field
pvault set-sensitivity <id> <tier>       # Set sensitivity tier
pvault export                            # Export all fields as JSON
pvault backup [--to <folder>]            # Encrypted backup now; --to sets the auto-backup folder
pvault restore <file> [--check] [--ask-key]  # Recreate vault from a backup / only verify it
pvault audit                             # Show access audit log
pvault status                            # Show vault status
pvault expiring [days]                   # Expired/expiring *data_scadenza fields and pratiche
pvault doc add|list|get|delete           # Encrypted document scans
pvault person list|show|add|remove|migrate  # Other people, e.g. family (leo:cie.numero); alias: member
pvault touchid enable|disable|status     # Touch ID approvals (macOS)
pvault create-service-token|list-service-tokens|revoke-service-token
pvault ui                                # Onboarding page in the default browser
```

`pvault help` prints every command with its flags.

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
- Vault key exists only in memory while unlocked. `requireUnlocked` hands out a copy
  that each caller clears (`defer clear(vaultKey)`, and its subkeys likewise); callers
  that don't need the key use `checkUnlocked`
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
  block DNS rebinding. Grant and unlock attempts are counted on entry and refunded unless
  the password was wrong, so concurrent guesses can't bypass the limit and junk requests
  can't use up the user's own attempts.
  API tests build requests with `newTestRequest` (loopback Host)
- Writing a field without a tier keeps its existing tier (schema default for
  new fields), so overwrites never silently downgrade sensitivity
- Secret key at `~/.pvault/secret.key` (0600), never in database. `Init` never replaces an
  existing one (it may be the only key to old backups) and removes a half-made vault on failure.
  `Open` never creates `vault.db` (`ErrNotInitialized`), so `pvault serve` in an empty dir
  can't make a later `init` refuse
- Credentials (password + secret key) never go over TCP, where anything can listen on
  7200 while the server is down. `pvault serve` also listens on `<vault dir>/run/pvault.sock`
  (dir 0700, socket 0600; bound before TCP, never taken over from a live server, stale
  ones replaced). `reUnlock`/`requestGrant` use `credentialRequest` (`socket.go`), which
  refuses a run dir that is a symlink, not ours or not 0700, and checks the listener
  (`peer_darwin.go`: `LOCAL_PEERCRED` uid, kernel exe path via `proc_info` equal to our own
  resolved `os.Executable()`, and the same running cdhash as this process via `csops`, so a
  server left running across a reinstall is refused before any prompt; Linux: `SO_PEERCRED`
  + `/proc/<pid>/exe`; other OSes keep TCP). `checkVaultServer` runs before any password or
  Touch ID prompt, and also requires `VAULT_ADDR` to be the TCP address the verified server
  reports in `/vault/status` (`addr`), since tokens and the UI's password go there. The Go side doesn't require the hardened runtime, so `go build` and
  release binaries work; the helper does (`make build` signs pvault that way).
  Limits: the private run dir is the protection. The peer check catches stale or
  mismatched servers but is not a guarantee against a process that can already write to
  the vault dir, nor against one that replaces the pvault binary (`~/.local/bin` is
  user-writable). The onboarding page still sends its password over TCP (browsers can't
  use Unix sockets). `pvault lock` signals the PID file's process only if its kernel exe
  path is this pvault. A server that exits during startup reports its own last stderr line.
  Tests: `serveOnSocket`/`shortVaultDir` (t.TempDir paths exceed the 104-byte socket limit)
- Touch ID (`cmd/pvault-touchid/main.swift`, macOS, ad-hoc signed + hardened runtime):
  `pvault touchid enable` (TTY only) stores the profile password in a plain login-Keychain
  item (service `pvault-touchid`, account = vault dir) whose ACL trusts only that exact
  helper build. Biometric Keychain flags need a paid Apple signature (-34018 otherwise),
  so the helper checks Touch ID itself (biometrics or Apple Watch, no Mac-password
  fallback), then sends the password only over the vault socket and returns only a token.
  The Keychain item holds JSON `{password, server}` and `kSecAttrGeneric` =
  `pvault-touchid/2`: `touchid enable` passes pvault's own resolved path (`enroll --server`,
  which fully validates that file), and the helper sends only to a listener whose audit
  token (`LOCAL_PEERTOKEN`) gives a valid, hardened `SecCode` with that file's declared
  cdhash (no per-request file hashing). An item without the marker (older helper) counts as
  not enrolled, so no fingerprint is asked for. The helper's HTTP client refuses chunked
  bodies (the server sends Content-Length). Its prompt is worded from the exact scope it requests:
  append/write grants say they can read too, `cat.*` says "all your cat fields and
  documents" (`leo:cat.*` "all of leo's ...", `*:cat.*` "everyone's ...", `leo:*`
  "everything about leo"), `*` is named alone and says it includes the family, durations round up, and patterns are anchored with `\A`/`\z` (ICU `$` lets
  a trailing line break through) and capped at 100 characters.
  pvault looks for the helper only next to its own binary. CLI commands call
  `needGrant(scope, access)`; with Touch ID on, the grant is scoped to that (full access
  if unset), and in a TTY a failed or denied prompt falls back to the typed password.
  When a scoped grant gets 403 `session_required` (an operation only the user's session
  may do), `authed` (`helpers.go`) asks once more for full access and resends.
  `pvault ui` and `onboard` (before its grant) run `checkVaultServer` too, so the password
  page never opens on an impostor and no grant goes to an unverified address.
  `pvault unlock` with Touch ID starts `pvault serve --locked` and the helper unlocks it.
  Rebuilding the helper changes its signature, and moving or rebuilding pvault changes
  the trusted server, so re-run `pvault touchid enable`. The helper never talks TCP: the
  token it returns goes to `serverAddr()`, which `checkVaultServer` (run before every
  prompt) has checked is the socket-verified server's address, so any vault's port works.
  Prompts name the vault from its dir (`.pvault-studio` → "your studio vault"; "your
  vault" otherwise). A `unlock` that started the server stops it again if no one
  can unlock it. `touchid enable`/`disable` are interactive only, and the helper's own
  `enroll`/`remove` refuse unless stderr is a terminal, so an agent can't switch it off.
  Debug builds (`-D DEBUG`) honor `PVAULT_TOUCHID_TEST_APPROVE=1` and
  `PVAULT_TOUCHID_TEST_PASSWORD`/`_SERVER` (enrollment without the Keychain) for tests, and
  add `describe-grant` (prints the prompt; `TestTouchIDHelper_*` compiles it with swiftc and
  runs it against real `pvault serve --locked` builds and impostors)
- Scoped grants: `POST /vault/session` with `scope` (+ `access`) issues a grant limited
  like a service token (TTL max 10m) that can reach `personal` fields, because the user
  approved it in person. It is not session auth (no audit, lock, tiers or tokens), and only
  its creation resets the auto-lock timer.
- Only session auth may lower a tier: a token or scoped grant writing a field with a
  `sensitivity` below its current tier (or the schema default) gets 403, and so does
  deleting a field or document stored above its default tier (re-creating it would lower it).
  A tier-less write keeps the stored tier in the same SQL statement (`store.Field.KeepTier`),
  so it can't undo a concurrent raise. A token's write or delete is conditional on the field
  it checked (`SetChecked`/`DeleteChecked`, `store.Seen`): if the field appeared or changed
  tier meanwhile, it gets 409 instead of landing. Defaults ignore ID case, like validation.
  For tokens, DELETE of a missing field is 404, the same as a hidden personal one, and a
  read-only token's PUT is refused before the field is looked up; for the session, a
  delete or tier change of a missing field is 404 too.
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
  `pec_attachment`/`pec_save_attachment` (writes into the first import dir, `wx` + 0600, then
  sets `com.apple.quarantine` so Gatekeeper warns on open; names are cut to 200 UTF-8 bytes
  with control characters removed, so `vault_document_add` accepts them), registered only
  when `PEC_ADDRESS` is set. No SMTP code by design. Only png/jpeg/gif/webp go out as inline
  images (`content.ts`); HEIC, TIFF and the rest as an embedded resource.
  EXAMINE + BODY.PEEK so nothing is marked read. The app password comes from Keychain
  (`PEC_KEYCHAIN_SERVICE`, account = the PEC address) per request, never cached.
  Messages are unwrapped from `postacert.eml` and `daticert.xml` is parsed
  (mailparser needs `ignoreEmbedded: true` or it splices the inner message into the envelope)
- Italian fields are validated/normalized in `Vault.Set` (`internal/vault/italy.go`);
  SPID/CIE credentials (`IsCredentialField`: a credential word, plurals, digits and
  `pincode`/`pukcode` included, with spid/cie/cieid among the category's or field's
  words, a trailing number allowed: spid2, cie2) can only be stored with tier `personal`;
  categories whose first word is `spid`/`cieid` (`spid_poste`, `spid2`) default to it, and so
  do their documents (`DefaultDocumentTier`, which the upload check uses, so a token can't
  create a personal document in `cie_pin` either). `personal` (rank 4) is
  never reachable by service tokens: they can't be capped at it, and the API hides
  personal items from them (404 on read/delete, 403 personal_only on write, omitted
  from every listing; a single-field read re-checks the tier on the row it decrypts,
  `GetIf`). Only session auth and scoped grants the user approved reach it. Portal login: `preferences.accesso_pa` (`spid`/`cie`) and
  `spid.provider` (fixed spellings matching the onboarding select,
  `TestOnboardingSelectOptionsValidate`) are validated in `access.go`; the provider
  stays lowerable to `standard` so agents can pick it. Fields named with the word
  shape `data_<x>`, `date_of_<x>` or `<x>_date` must be real dates, stored as
  YYYY-MM-DD (`dates.go`)
- Housing categories `abitazione`, `locazione`, `utenze`, `tari` describe the home at
  residenza/domicilio (address not duplicated). Their format rules live in
  `internal/vault/housing.go` and run before the generic ones in `NormalizeAndValidate`,
  scoped to those categories. Euro amounts need 3-digit thousands groups and at most 2
  decimals (`1.234.56` and the ambiguous `1,250` are refused). Every onboarding `data-field` category must be in the
  page's `categories` list (`TestOnboardingFieldsAreLoaded`)
- `business` (incl. `sede_legale_*`) and `locale` (the business premises, one unit, with its
  own address) have their rules in `internal/vault/business.go`; address parts share the
  residenza checks through `addressPart`, catasto fields share abitazione's (`normalizeCatasto`).
  `identity.birth_country` is for births abroad only (Italia is refused). These came from the
  pratiche-napoli library's `contract/extensions.json`: when the library needs a field the
  vault lacks, add it here, then remove it from `extensions.json` there and regenerate `fields.json`
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
  may upload below, or delete above, the category's `DefaultDocumentTier` (`sensitive`,
  personal for SPID/CieID); others get 403 session_required.
  Downloads are served as attachments with `Content-Security-Policy: sandbox`
- Backups (`backup.go`): `vault.db` snapshot (`VACUUM INTO`) + `documents/*.enc` in a tar,
  encrypted again with chunked AES-256-GCM (`crypto/stream.go`, STREAM nonces) under
  HKDF(vault key, salt, `backup/<file id>`), so field names and the audit log are hidden.
  The plaintext header (magic, version, time, salt, file id) is the AAD. `secret.key` is never
  in a backup. `POST /vault/backup` is session-only; `--to` stores the folder in meta
  `backup_dir` (absolute, outside the vault dir after resolving symlinks). Every write calls
  `markChanged`, which backs up `backupDelay` (1m) after the last change; `Lock` (holding
  `backup.mu` throughout) and auto-lock flush a pending one first, so `pvault lock` waits
  for the server to exit. Lock order: `backup.mu` → `writeMu` → `v.mu` → session `mu`; `backup.stateMu`
  is a leaf, so `markChanged` never waits on a running backup. Auto-lock ignores
  stale timer firings (`Touch` arms a new timer; an older one's generation no longer
  matches) and only clears its own session. Files are
  `pvault-<vault id>-<time>.pvbackup` (id = SHA-256 of the salt, 8 hex); pruning keeps this
  vault's 10 newest plus the newest per day for 30 days and never touches other files.
  Documents whose file is gone are dropped from the snapshot rather than failing the backup.
  `store.PrepareSnapshot` also drops every service token (a restore must not revive a
  revoked one) and records the backup's own time as `backup_last`.
  Each backup first removes `.backup-*` work dirs a crash left in the vault dir (they hold a
  plaintext-named snapshot); restore and check remove `.pvault-restore-*` / `pvault-check-*`
  ones older than 10 minutes. `SetBackupDir` creates the folder (0700) only after checking it.
  `RestoreBackup` only fills a missing or empty dir and checks the result unlocks
  (`--ask-key` tests a hand-kept secret key); public status shows backup state, never the folder

- People (`scope.go`, `people.go`; the family view in `famiglia.go`, `household.go`): besides
  the owner, a vault holds other people (today: family members). A person's IDs are
  `<slug>:category.field` (slug `^[a-z][a-z0-9-]{0,31}$`, `io` reserved for the owner, who
  has no prefix); documents in `<slug>:<category>`. The general layer says "person"
  (`SplitPerson`, `ValidPersonSlug`, `RemovePerson`, `store.PersonRows`); `pvault person`
  is the command, with `pvault member` as an alias. The API route `/vault/members`, the
  JSON keys (`member`, `members`, `"for": "member"`), the audit actions (`member_remove`)
  and the MCP tool `vault_household` keep their old names. The stored category (`leo:cie`)
  is the HKDF info, so each person has their own subkeys and no DB change was needed. Every
  category-keyed rule (`IsCredentialField`, `IsPersonalCategory`, `DefaultSensitivity`,
  `DefaultDocumentTier`, `NormalizeAndValidate`, `SuggestCanonical`) strips the prefix
  first with `SplitPerson` (whatever the slug looks like), so `leo:spid.password` is
  personal like `spid.password`; `TestPersonRulesMatchOwner` checks every schema ID and the
  credential tables with a prefix. Owner-only categories (housing, `pratiche`) refuse a
  prefix. The schema is the only list of owner-only and person-only fields
  (`SchemaField.For`: `ForOwner`/`ForPerson`, enforced by `checkFieldFor`) and of fields
  whose value names people (`PersonRef`: `famiglia.genitori`, `famiglia.intestatario_scheda`;
  plus pratiche `persona`, in `isPersonRefField`). Such values must name `io` or an
  existing person. Scopes: unprefixed patterns are the owner's only; `leo:cat.*`,
  `leo:cat.field`, `leo:*` and `*:cat.*` (owner and every person) reach other people; `*`
  everyone; `*:*` and `io:` are refused. Go, `mcp/src/scope.ts` and the Touch ID helper
  parse the same grammar, and the Go and TS matchers share
  `internal/vault/testdata/scopes.json`. `famiglia` holds facts only (relazione → ANPR
  Tabella 05 code and gendered label, stato_famiglia, a_carico, genitori, rappresentanza;
  the owner's `intestatario_scheda`); procedure rules live in playbooks. `GET
  /vault/members` (`Household`) derives age/minor/label only from fields the caller can
  read. `MigrateNucleo` is session-only; `DELETE /vault/members/{slug}` also accepts a grant
  the user approved with write access to `<slug>:*` (`ScopeAllowsWholePerson`), so `pvault
  person remove` asks one Touch ID prompt worded "read, change and delete everything about
  <slug>" (service tokens never may). Both hold `backup.mu` and `writeMu` (every single
  write holds `writeMu` for reading), change rows in one `store.ApplyPersonChange`
  transaction; remove refuses while another field names the person (closed pratiche don't
  count), then revokes tokens and drops grants whose scope names the slug. The public
  status counts only the owner's data, so it never reveals other people. Bulk reads parse
  the scope once (`ParseScope`). No slug rename yet. Business and client vaults:
  `docs/design/business-vaults.md`.
- Vault kinds and named vaults (`kind.go`, `ruolo.go`, `cmd/pvault/vaults.go`): meta `kind`
  (`persona`, the default and every vault from before kinds, or `impresa`, set by
  `pvault init --impresa` via `InitWithOptions`) is loaded by `Open` and reported in
  `/vault/status` (`kind`) and `/vault/schema` (`vault_kind`). Schema categories carry
  `subject` (persona/impresa) and `kinds`; fields may carry `kinds`
  (`business.qualita_dichiarante` is persona-only). Additive keys only: pratiche-napoli's
  `gen-fields` refuses new `scope` or `for` values. `checkKind` (in `Vault.set` and
  `AddDocument`, not the pure `NormalizeAndValidate`, so `TestPersonRulesMatchOwner` holds)
  refuses a category kept for the other kind (`famiglia` persona, `ruolo` impresa), a
  persona-subject category in an impresa vault's own data (numbered copies such as `cie2`,
  `identity-2` count as their category, `schemaCategoryOf`), in an impresa vault any
  person's SPID/CieID category or credential field/document (CIE and `firma`/`cns` PINs
  included), even at the personal tier, and in an impresa vault's own data any credential or
  personal category except its own SPID (`isSPIDCategory`): a CIE, CieID or digital
  signature is always a person's. `MigrateNucleo` refuses in impresa vaults.
  `ruolo.tipo` is a validated list (`normalizeRuoli`); the people listing carries `ruolo`
  and `denominazione`. Named vaults: `--vault <name>` (first argument only) or
  `PVAULT_VAULT` becomes `VAULT_DIR=~/.pvault-<name>` via `os.Setenv` before dispatch, so
  `serve` and the Touch ID helper (which read only `VAULT_DIR`) follow; a different
  `VAULT_DIR` is an error. Each vault's TCP port is in a plain `<dir>/port` file (0600):
  init and restore write it (`ensurePort`: 7200 for `~/.pvault`, else the lowest free
  7201-7299 no sibling vault's port file names), `serve` and `serverAddr()` read it, and the
  CLI never opens `vault.db` for it; it isn't trusted, since `checkVaultServer` compares it
  with the socket-verified status `addr` (and records the verified `kind` for `person`).
  Hints in messages use `pvCmd` so they name `--vault`. Tests: `kind_test.go`,
  `vaults_test.go`, `TestTouchIDHelper_PromptNamesVault`, `TestTouchIDHelper_UnlocksNamedVault`.
  The onboarding page hides what a person can't hold from `/vault/schema`'s `subject`/`kinds`,
  mirroring `checkKind` in JS (`allowed`/`inputAllowed`); no test runs that JS, so change both
  together (`TestOnboardingRuoliComplete`/`TestOnboardingKindMarkers` check only the option
  lists and markers).

## Conventions

- Field IDs are `category.field_name` (e.g., `identity.full_name`), or
  `person:category.field_name` for another person (`leo:identity.full_name`)
- Sensitivity tiers: `public`, `standard`, `sensitive`, `critical`
- All timestamps stored as RFC3339 strings in SQLite
- WAL mode enabled, busy_timeout=5000ms; the vault path can't contain `?` (the driver
  cuts its DSN there)
- CLI arguments: commands without flags use `positionalArgs`, so an unknown option is an
  error, never ignored or read as a name; `pvault set` takes one value (quote spaces).
  `parseFlags` refuses a repeated flag, and `serve` accepts only `--password-stdin` or
  `--locked`; the stdin password is kept as typed (only the line ending is dropped)
- No CGO — pure Go for portability

## Environment Variables

- `VAULT_DIR` — vault directory (default: `~/.pvault`)
- `PVAULT_VAULT` — like `--vault <name>`: the vault in `~/.pvault-<name>`
- `VAULT_ADDR` — server address for CLI (default: from the vault's `port` file, else `http://127.0.0.1:7200`)
- `VAULT_PORT` — server port for `pvault serve` (default: the vault's `port` file, else `7200`)

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
