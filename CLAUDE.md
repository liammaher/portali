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
pvault init [--impresa] | onboard        # Create new vault (~/.pvault/); --impresa: a business vault
pvault --vault <name> <command>          # Any command on another vault (~/.pvault-<name>)
pvault unlock | lock | serve             # Background server / stop it, zero keys / foreground
pvault set <category.field> <value>      # Set encrypted field
pvault get <category.field|category.*> [...]  # Get decrypted field(s); several → JSON, one grant
pvault list [category]                   # List fields
pvault delete <category.field>           # Delete field
pvault set-sensitivity <id> <tier>       # Set sensitivity tier
pvault schema [--json]                   # Recommended fields; --json is schema.json
pvault locate [residenza|...] | --street # Municipalità, quartiere, distretto ASL of a Naples address
pvault export | audit | status           # All fields as JSON / access log / status
pvault backup [--to <folder>]            # Encrypted backup now; --to sets the auto-backup folder
pvault restore <file> [--check] [--ask-key]  # Recreate vault from a backup / only verify it
pvault expiring [days]                   # Expired/expiring *data_scadenza fields and pratiche
pvault doc add|list|get|delete           # Encrypted document scans
pvault sign list|review|reject           # Signing requests an agent filed: you review the signed PDF and approve it
pvault files source|sync|search|link ... # File index: where files are, linked to people, clients, pratiche
pvault person list|show|add|remove|export|migrate  # Other people (leo:cie.numero); alias: member
pvault retention [set <years>|unset]     # How long people's data is kept after ruolo.data_fine (no default)
pvault retention scans <days>|unset      # How long clients' ID and procura scans are kept after their pratiche close (no default)
pvault touchid enable|disable|status     # Touch ID approvals (macOS)
pvault hello enable|disable|status       # Windows Hello approvals (Windows)
pvault credential read|write <name>      # A secret in Windows Credential Manager (for the MCP server)
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
  store/             SQLite CRUD (fields, documents, people, tokens, audit, meta, file index)
  vault/             Business logic: rules, encrypt/decrypt, sessions, people, backups
  winhello/          Windows Hello prompts from pure Go (WinRT UserConsentVerifier)
  wincred/           Windows Credential Manager from pure Go (the Keychain's place there)
  api/               HTTP server, handlers, auth middleware, onboarding page (ui/)
  napoli/            Street → municipalità → ASL distretto, offline (vault: located.go)
  filesync/          File index sources for `pvault files sync`: a folder, or OneDrive/SharePoint,
                     Google Drive and Dropbox over OAuth
mcp/                 MCP server (TypeScript): vault tools over a service token, read-only PEC
claude/skills/       vault and pratica skills; pratica keeps one playbook per procedure
  pratica/fill/      fill.py: fills a library form from the vault, prints one channel's
                     route, or the person's desk in each of the library's office lists
                     (one per service); pilot/ has the pilot forms. A real vault only over its socket
  pratica/portal/    run.py: fills a portal pratica from a steps file (rehearsals/*/portal-steps.yaml)
                     with inpage.js, in a Playwright window (`run.py run`) or the Claude browser
                     pane (`run.py page`); reads every field back, then again on a fresh load of
                     the draft, attaches the files it names (the pane: 256 KB in all), never
                     declares, pays or sends.
                     Both take a draft's values from one `pvault get` (`--values -`, Touch ID)
  pratica/review.py  the page the person reviews: what's left for them, the checks, every value
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
- On Windows the channel is a named pipe (`transport_windows.go`). Pipe names are machine-wide
  and any account can make one, so the server picks a random name, writes it to
  `<vault dir>\run\pipe` (a `RestrictDir` folder) and holds that file open while it serves;
  a file Windows lets you delete means the server is gone. The MCP server and fill.py use a
  name only while that delete fails (`EBUSY`, error 32). The pipe's DACL lets only this user
  in, remote clients are refused, and the CLI checks the server's token SID and exe path
  (`GetNamedPipeServerProcessId`; no cdhash, as on Linux). Any other OS has no channel and no
  server: `serve` and the commands needing the password refuse (`socket_other.go`).
- Limits: this catches stale or impostor servers, not a process that can already write to
  the vault dir or replace the pvault binary (on Windows: any process of this user).
- `pvault lock` needs no trust: a server on the vault's socket that this build won't send a
  password to (another build, e.g. after a reinstall) is still stopped, by SIGTERM, which the
  server takes as lock (`untrustedSocketServer`); stopping a server never needs a password.
  Windows has no SIGTERM, so there lock sends `POST /vault/stop` over the pipe, which only
  the private channel reaches and which also takes no password.

### Touch ID (`cmd/pvault-touchid/main.swift`)
- The profile password sits in a login-Keychain item whose ACL trusts only that helper
  build; the helper checks Touch ID itself, sends the password only over the socket to the
  enrolled pvault (audit token → valid hardened code with the enrolled cdhash), and returns
  only a token. Rebuilding the helper or moving pvault means `pvault touchid enable` again.
- The prompt is worded from the exact scope it requests and never understates it.
  Patterns are anchored with `\A`/`\z`. Its parser is held to `internal/vault/testdata/scopes.json`,
  its wording to `prompts.json`, as is the vault's own (`approval_prompt.go`, for Windows Hello).
- `touchid enable`/`disable` refuse without a terminal, so a script can't switch them; an
  agent with a pty could, which only falls back to the password.
- CLI commands call `needGrant(scope, access)`: with Touch ID the grant is scoped to that
  (full access if unset). A scoped grant refused with 403 `session_required` makes `authed`
  ask once more for full access.

### Windows Hello (`internal/winhello`, `approvedGrant` in `api/handlers.go`)
- The vault server asks itself: `POST /vault/session` with `approve: "hello"` (private channel
  only, never with a password, only once the session turned it on with `POST /vault/hello`,
  itself private-channel only; meta `hello`, which backups clear, so a restore starts with it
  off) shows a prompt `pvault wants to <reason>.`, the reason from `GrantPrompt` for
  the validated request or `SignPrompt` from the vault's own summary, and issues exactly that
  grant (`CreateApprovedGrant`, `CreateApprovedSignGrant`; audited `via: hello`). Nothing
  secret is stored for it, and `unlock` keeps the password.
- Trusting the yes/no inside the server adds nothing: it already holds the vault key. Any
  local caller can only make a truthful prompt appear. A request that can't be granted as
  asked is refused before anyone is asked; refusals are audited (`approval_refused`) and
  count toward the grant limit; one prompt at a time (409). A prompt is cancelled when its
  caller leaves or the server stops, and an approval grants nothing if Hello was turned off
  or the vault locked meanwhile.
- `winhello` calls WinRT through vtables: out-pointers go through `object.call`
  (`//go:uintptrescapes`), and results start as "not verified", so a lost answer refuses.
- The CLI asks through one approver (`approve.go`): Touch ID on macOS, Windows Hello on
  Windows. Status over the private channel says `approver: "hello"`; the client checks the
  echoed scope, access and sign as the helper does.

### Who may do what
- **Session** (the user, typed password): everything. Handlers test `isSessionAuth`.
- **Scoped grant** (`POST /vault/session` with `scope`, max 10m): what the user approved
  in person (Touch ID or Windows Hello), `personal` tier included. Not session auth.
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
- Lock order: `backup.mu` → `writeMu` → `filesMu` → `v.mu` → session `mu` → `signingStore.mu`
  (lock and auto-lock hold `v.mu`, so they only `TryLock` `filesMu`). `backup.stateMu` is a leaf.
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
  `pratiche` (one per holder) have none (`oneOnly`, `one_only` in `schema.json`); the
  library names a second premises `locale2.*`.
- **Credentials** (`IsCredentialField`: a secret word like pin/puk/password/otp in the
  category or field name, wider words next to spid/cie/firma) are stored only as
  `personal`; SPID/CieID categories and their documents default to it.
- **Formats**: Italian codes, dates (`YYYY-MM-DD`), euro amounts and areas are validated
  and normalized (`italy.go`, `dates.go`, `housing.go`, `business.go`). Housing categories
  (`abitazione`, `locazione`, `utenze`, `tari`) describe the home at residenza/domicilio;
  the schema exports them as `kinds: [persona]` (set from `IsHousingCategory`).
  New fields the pratiche-napoli library needs are added here, then dropped from its
  `contract/extensions.json`. `schema.json` is `pvault schema --json`, credential words
  included (`Credentials`, the expressions `IsCredentialField` compiles), and `values` for
  the fields with a fixed list (`ruolo.tipo`, an address's municipalità and distretto:
  `schemaValues`) and the pratiche stato and canale lists (`Pratiche`: closed stati, the
  library's channel types as aliases), which the library and fill.py read instead of keeping a copy. It is written without
  HTML escaping, so the library's copy is the same bytes; its CI compares it with main's.
- **Pratiche** (`pratiche.go`): `pratiche.<slug>_<campo>`, campo/stato/canale from fixed
  lists (canale also takes the library's channel types, `praticaCanaleAliases`), never
  credentials. A deadline shows in `Expiring` until the stato closes. The
  store dates each closing (`closed_at`, kept across corrections between closed stati).
  `praticheIn` is the one reading of "is this pratica closed, and for whom".
- **Documents** (`documents.go`): AAD bound to the document ID, encrypted filename,
  `documents/<id>.enc` (0600); token scope ID `<category>.documents`. Downloads are
  attachments with `Content-Security-Policy: sandbox`.

### Sign gate (`signing.go`, `pdfstamp.go`, `api/signing.go`, `cmd/pvault/cmd_sign.go`)
- An agent files a request (PDF + where the signature goes); the vault stamps the owner's
  handwritten signature (category `firma_autografa`: one PNG with a transparent background,
  always `personal`) and today's date first, so **the file the user reviews is the file
  approved** (compared by sha256). Requests live in the `Session` only (never disk, backup
  or the database), for the same calendar day at most; a lock drops them. Their limits are in `docs/usage.md`.
  A request belongs to its token (its hash, not its consumer name; `session`, `grant` and
  `vault` are reserved names). Only the owner (`io`) of a personal vault signs so far.
- A token (scope `firma_autografa.signing`, append) can request, list and withdraw *its own*
  requests; it can't read the signed file or approve. Approval needs the user's session or a
  **sign grant**: `POST /vault/session` with `sign: <id>` and the password, socket only
  (`viaSocket`), one request, ended by its approval. `ValidateToken`/`ScopedGrant`
  ignore it and the middleware lets it reach only that request's `candidate` and `approve`.
  Every request, grant, view, approval and refusal (`sign_refused`) is audited with the
  file's sha256. PDFs are screened before stamping and stamped under a timeout. Both clients
  check the grant echoes `sign` (an older server would issue full access).
- `pvault sign review` is the human act: grant, fetch, open, type the first 8 hash characters,
  approve, save (never over a file). With Touch ID the fingerprint is the approval, asked
  before the PDF is shown (an agent driving review or the helper can read the typed code; the
  password route can't be driven that way), so the prompt is what the user must read: worded
  from the vault's own summary (name, pages, hash, who filed it), never from arguments
  (`describe-sign` and `vault.SignPrompt` held to `testdata/prompts.json`).
- Not a firma digitale. Procedures needing one stay `human_only`. What a channel takes is
  the form's own `signing` for it, else the library's `contract/signing.json` (the law's
  rule per channel type; fill.py `signing_rule`); where neither has one, the step is the
  user's, as the channel asks.

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
- `identity.display_name` (`public`) is what agents call a person; the household view
  gives it, the owner's as `owner`, so `*:identity.display_name` is enough to ask whom a
  form is for.
- A closed pratica may keep naming a removed person; reopening it, by writing its stato
  or deleting it, checks `persona` again (`checkPraticaPersona`, whatever the caller reads).
- Old JSON and audit names stay for compatibility: `/vault/members`, `member`, `members`,
  `"for": "member"`, `member_remove`, MCP `vault_household`.

### Naples address lookup (`internal/napoli`, `vault/located.go`)
- `internal/napoli` works from the embedded `data/stradario.json`, built by `gen/gen.py` from
  the Comune's stradario PDFs (2025 for municipalità, 2003 COSAP for civico ranges). Never a
  geocoder: the home address goes nowhere. A street crossing a municipalità boundary (about
  4%) is `Ambiguous` until a civico in a known range or a named quartiere decides it; a
  lookup never guesses. One ASL (Napoli 1 Centro) serves the whole comune; its distretto
  follows the municipalità.
- Each address (`residenza`, `domicilio`, `locale`, the sede legale) has computed
  `quartiere`, `municipalita`, `distretto_asl` (`derived` in schema.json, standard tier).
  `GetIf`, `GetByCategory` and `GetContext` answer them; `List`, export, backups and person
  export hold stored rows only (`FieldInfo.Derived`; `pvault export` drops them).
- A stored row overrides the computed value only while it is not older than the address's
  street, civico and comune (the distretto: nor than the stored municipalità); an older one
  is ignored, never used with a warning. Nothing is computed unless the comune is Napoli.
- A computed field is read under its own scope and tier, never the street's: a token scoped
  to `residenza.municipalita` never sees the street, and the audit log names the computed
  field. A personal address row never feeds a caller that can't see personal items, and a
  stored row the caller can't see is not replaced by a computed value.
- `pvault locate` and `GET /vault/located/{target}` only read, with a grant for the three
  computed fields; they never echo the street. The answer to a street across a boundary is
  the quartieri to store (`pvault set residenza.quartiere ...`).

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
  `scan_days`, set only by the session. What `Expiring` then reports (`retention`,
  `pratiche_closed`, `files_closed`) is in `docs/usage.md` (Retention).
- Nothing is ever deleted automatically; an indexed file is reported only as a reference
  (the vault can't delete it).
- A scan or indexed file is reported only if older than the closing it waits for (a
  same-second one counts as after; a file linked to several clients, the earliest of their
  last closings), a procura's only once its `data_scadenza` has passed (none: still in
  force). A caller that can't see everything relevant gets no entry.
- `GET /vault/members/{slug}/export` is session-only and audited; personal-tier items only
  with `include_personal=1`. `pvault person export` writes a new 0700 folder, files 0600.

### File index (`files.go` in store and vault, `docs/design/file-index.md`)
- Where a person's, business's or client's files are (disk, OneDrive, SharePoint, Google
  Drive, Dropbox) and what each is linked to. Metadata and links only: bytes and text never
  enter the vault, and the vault never goes online (a sync client writes the index; an API
  source's sign-in stays in the Keychain or Windows Credential Manager, `filesync.SystemStore`,
  and `source connect` is the user's to run).
- Own tables (`vault_file_*`), not documents or fields. Names, paths, URLs, sizes and
  hashes sit in blobs under info `files/index`, AAD `<kind>:<row id>`; plain columns are only
  holder, kind, pratica slug and tiers. No plaintext hash or search index.
- Scope IDs `files.index` / `<slug>:files.index`. A file with no link is the owner's; a file
  once linked to a person (`scoped`) stays out of the owner's scope. A login-secret word in
  the name or path makes it `personal`.
- Sources, links, rules, pruning and accepting suggestions or proposals need the session or
  an approved grant with write access and the holder's scope (`userWrite`); forgetting a
  source needs every holder's. A service token with write access may sync, but only files
  it can see (`UpsertFilesIf`); otherwise a token only suggests links or proposes files.
- A sync never finishes after an incomplete scan and never opens a file (online-only
  placeholders would download); one that would mark most of a source gone needs `confirm`.
- A file found through a connector is only a proposal until the user accepts it; it then
  lives in a `via: connector` source that nothing syncs or marks gone.
- Never store a sharing link (`CheckFileURL`). A file that left its source is tombstoned,
  never deleted; `PruneFiles` drops only unlinked tombstones. `RemovePerson` drops the
  person's links, rules and suggestions, not the index rows.

### MCP (`mcp/src`; the tools are listed in `mcp/README.md`)
- Signing (`vault_sign_request`, `vault_sign_status`) can ask and watch, never approve or read
  the signed file. `vault_locate` reads `/vault/located` under the token's scope.
- File index tools (`vault_files_search`, `vault_file_get`, `vault_file_suggest`,
  `vault_file_register`; `vaultfiles.ts`): metadata only, no tool reads or changes a file,
  and suggesting or registering only proposes. Every result carries the untrusted-names
  note; ids and person/pratica slugs are checked before any request; `local_path` is the
  source's root plus the file's path and never leaves the root.
- Uses only a service token (`VAULT_TOKEN`, else `VAULT_KEYCHAIN_SERVICE`, else
  `VAULT_TOKEN_COMMAND`), looked up per request; the server enforces its scope. On Windows
  `VAULT_KEYCHAIN_SERVICE` (and the PEC password) come from Credential Manager through
  `pvault credential read`, found only by absolute path (`PVAULT_BIN`, else PATH). It refuses
  to start with the old `VAULT_SCOPE` set.
- Sends every request, the token included, only over `<vault dir>/run/pvault.sock` (a dir
  of this user's, 0700), never TCP; the vault is `VAULT_DIR`, else `~/.pvault-<VAULT_NAME>`.
  A set `VAULT_ADDR` must be the address that server reports.
- PEC (`pec.ts`): read-only IMAP (EXAMINE + BODY.PEEK), registered only with
  `PEC_ADDRESS`, app password from the Keychain (Credential Manager) per request, no SMTP code by design. Saved
  attachments go to the first import dir (0600, quarantined). Mailparser needs
  `ignoreEmbedded: true` or it splices `postacert.eml` into the envelope.

## Conventions

- All timestamps are RFC3339 strings in SQLite, to the second.
- WAL mode, busy_timeout=5000ms; the vault path can't contain `?`.
- CLI arguments: unknown options and repeated flags are errors, never ignored
  (`positionalArgs`, `parseFlags`); `pvault set` takes one value.
- A comment says why; CLAUDE.md says what must stay true. Keep both short.
- Windows Credential Manager (`internal/wincred`) stands in for the Keychain: `--keychain`,
  file-sync sign-ins (chunked as in the Keychain), the MCP and fill.py token lookups. Unlike
  the Keychain it has no per-program access list: any program of the user can read it. It
  keys on the name alone, so `--keychain` refuses a name saved for another account, and a
  credential of another account reads as none; `pvault credential` never touches a file
  source's sign-in (`pvault-files-*`).
- Windows ignores 0700/0600, so a folder that must be private (the vault, its `run`, a
  restore, a person export, sign review's temp folder) also gets an owner-only DACL
  (`vault.RestrictDir`). It rewrites the access of what the folder holds, so `Init` on Windows
  takes only a new or empty folder.

## Environment Variables

`VAULT_DIR`, `PVAULT_VAULT`, `VAULT_ADDR` and `VAULT_PORT` are in `docs/usage.md`; the MCP
server's are in `mcp/README.md`.

## Testing

Tests use temp directories and run with `-race`. `internal/vault` and `internal/api` call
`crypto.UseFastKDFForTests` in `TestMain` (cheap Argon2id; it panics outside a test binary).
API requests go through `newTestRequest` (loopback Host); socket tests use
`serveOnSocket`/`shortVaultDir` (temp paths exceed the 104-byte socket limit). The Touch ID
tests compile the helper with `-D DEBUG`, which adds `describe-grant` and test enrollment
(`PVAULT_TOUCHID_TEST_*`). Keychain tests run only with `PVAULT_KEYCHAIN_TEST=1`. The fill
pilot's synthetic vault (`TestPraticaPilotVault`) runs only with `PVAULT_PILOT_DIR`; run the
pilots through `claude/skills/pratica/fill/pilot/regress.py` (`--keep DIR` keeps a case's
output; it needs `PRATICHE_NAPOLI_DIR` and PyMuPDF; `PILOT_FORM_PDF` gives the TARI Mod. 1
pilot's form from disk; the `sign` case needs a library whose layout has `signatures`).
CI runs `regress.py` against the library's main (`fill.yml`); the library runs it on its own changes.
The portal runner has offline tests on a fake portal: `claude/skills/pratica/portal/bench/test_inpage.py`,
run with the pratica venv's Python (`~/.cache/pratica-portal/venv`, Playwright's Chromium, headless; no
network, vault or model).

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
