# Usage

## Quick Start

```sh
make build && make install

pvault onboard               # Create vault, unlock, populate common fields (interactive)
pvault set identity.full_name "Mario Rossi"
pvault get identity.full_name
pvault list
pvault lock                  # Stop server, zero keys from memory
```

Or step by step:

```sh
pvault init           # Create vault at ~/.pvault/, sets profile password + secret key
pvault unlock         # Start server at localhost:7200, prompts for password
```

Save your secret key somewhere safe. You need both the profile password and the secret key to unlock the vault.

While the vault is unlocked, every CLI command that reads or changes data asks for your profile password again, in an interactive terminal only. The server swaps it for a 2-minute grant held in memory. `pvault ui` opens the onboarding page, which asks for the password itself and gets a 15-minute grant, so no token ever appears on a command line or in browser history. No session token is ever written to disk, so an agent with shell access can't reuse your session. `pvault status` and `pvault lock` need no password (lock stops the server it started; for a server run with `pvault serve`, lock asks for the password). Only wrong passwords count toward the 10-per-minute limit.

With Touch ID on (`pvault touchid enable`, macOS), a fingerprint replaces the password for `pvault unlock` and for data commands, including ones run by an agent without a terminal. Each approval covers only what that command needs (e.g. `pvault get identity.codice_fiscale` asks to read just that field) for 2 minutes, and the prompt says so. `pvault get` takes up to 10 field IDs, so one approval can cover every field a task needs. In a terminal, a denied or failed prompt falls back to the password. The few things only your own session may do (deleting a field or document stored above its default tier, or storing a document below it) need full access: the command then shows a second prompt asking for that. Touch ID only sends your password to the `pvault` build it was enabled with, over the vault's socket (see Security Model), so after rebuilding or moving `pvault` or `pvault-touchid` run `pvault touchid enable` again, then `pvault lock` and `pvault unlock`. `pvault touchid status` shows whether it's on; `pvault touchid disable` removes the saved password.

## Fields

Fields use dot notation: `category.field_name`.

```sh
pvault set identity.full_name "Mario Rossi"
pvault set identity.date_of_birth 15/06/1995    # stored as 1995-06-15
pvault set residenza.comune "Napoli"
pvault set residenza.provincia NA
pvault set employment.employer "Acme S.r.l."
pvault set preferences.timezone "Europe/Rome"
pvault get identity.full_name
pvault get identity.full_name residenza.comune      # Several fields as JSON (max 10)
pvault list                      # All fields
pvault list identity             # One category
pvault delete identity.date_of_birth
pvault export                    # All fields as JSON
```

You can use any category and field name. Run `pvault schema` to see recommended field names and their default sensitivity tiers. `pvault set` takes one value, so quote values with spaces; on every command an unknown option is an error, never ignored. Deleting or changing the tier of a field that doesn't exist is an error too.

## Sensitivity Tiers

Each field has a sensitivity tier that controls how it's shared with consumers.

| Tier | Examples | Who can read it |
|------|----------|-----------------|
| `public` | Language, timezone | Any service token whose scope covers it |
| `standard` | Name, comune, employer | Service tokens with the default cap (`sensitive`) or higher |
| `sensitive` | Codice fiscale, street address, phone, rent | Service tokens with the default cap or higher |
| `critical` | Payment card number | Only tokens created with `--max-sensitivity critical` |
| `personal` | SPID login, CIE PIN/PUK | Only you: your own session, or a Touch ID prompt you approve |

A token's `--max-sensitivity` is the highest tier whose values it can read. Fields above it still appear in `GET /vault/fields` (metadata only, no value), and reading one returns 403 `sensitivity_exceeded`; context, category reads and document listings leave them out. `personal` items are hidden from every token completely.

```sh
pvault set-sensitivity financial.iban critical
pvault set-sensitivity preferences.timezone public
```

## Dates and expiry tracking

Fields named in the date shapes `data_<x>` (Italian, e.g. `cie.data_scadenza`, `nucleo_familiare.membro1_data_nascita`), `date_of_<x>` or `<x>_date` (e.g. `identity.date_of_birth`) must be a real calendar date; hyphens count as underscores, and names like `date_format` are not treated as dates. Both `YYYY-MM-DD` and the Italian `DD/MM/YYYY` (also `DD-MM-YYYY`, `DD.MM.YYYY`) are accepted and stored as `YYYY-MM-DD`; impossible dates such as `31/02/2027` are rejected.

`pvault expiring [days]` lists every `*data_scadenza` field (permesso di soggiorno, passaporto, CIE, patente, tessera sanitaria, ...) that has expired or expires within the window (default 90 days), soonest first, plus the deadline of every open pratica (shown as `pratica <slug>`).

## Document scans

```sh
pvault doc add permesso_soggiorno ~/Desktop/permesso.pdf     # encrypt and store (max 20 MB)
pvault doc add patente patente-fronte.jpg --sensitivity critical
pvault doc list
pvault doc get <id> --out /tmp/permesso.pdf                  # decrypted copy, mode 0600
pvault doc delete <id>
```

Each file is encrypted with AES-256-GCM using the category's documents subkey and bound to its document ID (so encrypted files can't be swapped between records); the original filename is encrypted too. Files live in `~/.pvault/documents/<id>.enc`. Service tokens reach documents through the scope pattern `<category>.documents` (or `<category>.*`) and their sensitivity cap; documents default to `sensitive`. At most two uploads run at once. `pvault doc get` strips leading dots from stored names so a document can never be written out as a dotfile. After storing a scan, delete or securely archive the unencrypted original.

The MCP tool `vault_document_add` only reads files inside the folders in `VAULT_IMPORT_DIRS` (default `~/Downloads`), resolving symlinks and refusing hidden files, so an agent can't use it to read arbitrary local files.

New fields get the schema default tier (`standard` for fields not in the schema) — use `pvault schema` to see them. Overwriting a field without giving a tier keeps its current tier.

## Backups

```sh
pvault backup --to ~/Library/Mobile\ Documents/com~apple~CloudDocs/pvault-backups  # set the folder and back up now
pvault backup                           # back up now
pvault restore <file> --check           # verify the backup unlocks, restore nothing
pvault restore <file> --check --ask-key # same, typing the secret key you saved (tests your copy)
pvault restore <file>                   # recreate the vault (only into a missing or empty VAULT_DIR)
```

A backup is a snapshot of `vault.db` plus the encrypted documents, in a tar encrypted again with chunked AES-256-GCM under a key derived from your vault key, so field names and the audit log are hidden too. Files are named `pvault-<vault id>-<time>.pvbackup` and written with mode 0600. Once a folder is set (an absolute path outside the vault directory), the server backs up a minute after the last change and again before it locks; `pvault lock` waits for that. Pruning keeps this vault's 10 newest backups plus the newest per day for 30 days, and never touches other files. `pvault status` shows when the last backup ran or why it failed, never the folder.

`secret.key` is never in a backup: restoring needs your profile password and the secret key you saved at `pvault init`. Service tokens are left out too (a token revoked after the backup must not come back), so create new ones after a restore. A restore or check interrupted with Ctrl-C leaves a work folder (`.pvault-restore-*` next to the target, `pvault-check-*` in the temp folder); the next restore or check removes it. Backing up needs your own session (password or Touch ID), not a service token.

## Service Tokens

Service tokens let agents authenticate with scoped, time-limited, sensitivity-capped credentials. They follow the 1Password service account pattern.

```sh
pvault create-service-token tari-agent --scope "identity.*,residenza.*" --ttl 1h
pvault create-service-token checkout --scope "payment.*" --max-sensitivity critical --ttl 1h
pvault list-service-tokens
pvault revoke-service-token 3f9a1c2b7d4e...   # token_prefix from the list, or the full token
```

- `--scope` is required (`"*"` must be passed explicitly).
- `--ttl` defaults to `24h`, maximum `2160h` (90 days).
- Store secrets with `pvault set <id>` and no value: it prompts hidden (or reads one line from stdin), keeping them out of shell history. `pvault export` omits personal fields unless `--include-personal` is passed.
- `--max-sensitivity` defaults to `sensitive`; `critical` fields are hidden unless you opt in. It can't be set to `personal`: personal fields and documents (SPID login, CIE PIN/PUK, anything you mark `personal`) are invisible to every agent token — reads and deletes return 404, writes return 403 `personal_only`, and they never appear in lists, context, category reads, expiry reports or document listings.
- Service tokens cannot change sensitivity tiers, and can't delete a field or document stored above its default tier (writing it again would lower it).
- Unknown or misspelled options (e.g. `--max-sensitvity`) are an error, never silently ignored.
- `--access` defaults to `read` (read only). `append` also allows adding new fields and documents but never overwriting or deleting them; `write` allows everything. Tokens created before this option existed are read-only.
- Service tokens do **not** reset the 30-minute auto-lock timer; only your session does.

## HTTP API

The vault runs at `http://127.0.0.1:7200`. All protected endpoints require `Authorization: Bearer <token>`.

### Public

```
GET  /vault/status                       # { initialized, locked, field_count, documents, categories, backup? } (personal items not counted)
GET  /vault/schema                       # Recommended field names and sensitivity tiers
POST /vault/unlock                       # { password, secret_key } → { token }
POST /vault/session                      # { password, secret_key?, ttl? } → { token, expires_at } (unlocked vault only, max 1h)
                                         # + { scope, access? } → grant limited to that scope (max 10m), used by Touch ID;
                                         #   unlike a service token it can reach personal fields in its scope
```

`/vault/unlock` and `/vault/session` require `Content-Type: application/json`. The CLI sends them over the vault socket (`<VAULT_DIR>/run/pvault.sock`) on macOS and Linux; the same routes are served there and on TCP.

### Expiry

```
GET /vault/expiring?days=90              # Expired or expiring *data_scadenza fields and open pratiche deadlines
```

### Fields

```
GET    /vault/fields                     # List all field metadata (no values)
GET    /vault/fields/{id}                # Get field with decrypted value
PUT    /vault/fields/{id}                # { value, sensitivity? } — upsert
DELETE /vault/fields/{id}                # Delete field
GET    /vault/fields/category/{name}     # All fields in category with values
```

### Documents

```
POST   /vault/documents?category=<c>&name=<file>[&sensitivity=<tier>]   # body: the raw file, Content-Type its type (max 20 MB)
GET    /vault/documents?category=<c>     # List documents (metadata only)
GET    /vault/documents/{id}             # Download the decrypted file (attachment, CSP sandbox)
DELETE /vault/documents/{id}             # Delete
```

### Context

```
GET /vault/context                       # Full decrypted dump grouped by category
```

This is what consumers call. Returns:

```json
{
  "categories": {
    "identity": [
      { "id": "identity.full_name", "category": "identity", "field_name": "full_name", "value": "Mario Rossi", "sensitivity": "standard" }
    ],
    "preferences": [...]
  }
}
```

### Sensitivity

```
PUT /vault/sensitivity/{id}              # { tier } — update sensitivity
```

### Service Tokens

```
POST   /vault/tokens/service             # { consumer, scope, ttl?, max_sensitivity?, access? } → { token, scope, max_sensitivity, access, expires_at }
GET    /vault/tokens/service             # List active (unexpired) tokens (hash prefixes only)
DELETE /vault/tokens/service/{ref}       # Revoke by listed hash prefix (≥8 chars) or full token
```

### Session

```
POST /vault/lock                         # Lock vault, zero keys
```

### Audit

```
GET /vault/audit?limit=50                # Recent access log: [{ id, consumer, scope, action, purpose?, created_at }]
                                         # A token's request is logged as action api_access under its consumer name
```

### Backup

```
POST /vault/backup                       # { dir? } → { path } (your session only; dir also sets the backup folder)
```

## Security Model

```
Profile Password + Secret Key (128-bit)
  → Argon2id KDF (64MB, 3 iterations)
  → Vault Key (256-bit, in-memory only)
  → HKDF per category → Category Subkeys
  → AES-256-GCM per field (12-byte random nonce)
```

- Profile password is never stored, unless you turn on Touch ID (`pvault touchid enable`): then it is kept in your login Keychain, readable only by the signed `pvault-touchid` helper
- Secret key lives at `~/.pvault/secret.key` (mode 0600), never in the database or a backup
- Password and secret key go from the CLI to the server only over the vault socket, after checking the server is this same `pvault` (macOS and Linux)
- Vault key exists only in memory while unlocked, zeroed on lock
- Auto-lock after 30 minutes of inactivity
- Every access logged to `vault_access_log`

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `VAULT_DIR` | `~/.pvault` | Vault directory |
| `VAULT_ADDR` | `http://127.0.0.1:7200` | Server address for CLI |
| `VAULT_PORT` | `7200` | Server listen port |

## File Layout

```
~/.pvault/
├── vault.db       # SQLite database (encrypted fields)
├── secret.key     # 128-bit secret key (mode 0600)
├── documents/     # Encrypted document files, <id>.enc (mode 0600); created with the first document
├── run/           # pvault.sock, the server's credential socket (dir 0700, socket 0600)
└── pvault.pid     # PID of the server 'pvault unlock' started (not written by 'pvault serve')
```
