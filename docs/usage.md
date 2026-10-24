# Usage

## Quick Start

```sh
make build && make install

pvault onboard               # Create vault, unlock, populate common fields (interactive)
pvault set identity.full_name "Cool Cucumber"
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

## Fields

Fields use dot notation: `category.field_name`.

```sh
pvault set identity.full_name "Cool Cucumber"
pvault set identity.date_of_birth "1995-06-15"
pvault set addresses.home_city "San Francisco"
pvault set employment.employer "Acme Corp"
pvault set financial.filing_status "single"
pvault set preferences.timezone "America/Los_Angeles"
pvault get identity.full_name
pvault list                      # All fields
pvault list identity             # One category
pvault delete identity.date_of_birth
pvault export                    # All fields as JSON
```

You can use any category and field name. Run `pvault schema` to see recommended field names and their default sensitivity tiers.

## Sensitivity Tiers

Each field has a sensitivity tier that controls how it's shared with consumers.

| Tier | Examples | Behavior |
|------|----------|----------|
| `public` | Name, timezone, language | Auto-shared with authorized consumers |
| `standard` | Address, employer, education | Shared on request, logged |
| `sensitive` | DOB, phone, tax status | Requires explicit approval |
| `critical` | SSN, card number, card expiry | Requires approval + verification |

```sh
pvault set-sensitivity financial.ssn critical
pvault set-sensitivity preferences.timezone public
```

## Dates and expiry tracking

Fields named in the date shapes `data_<x>` (Italian, e.g. `cie.data_scadenza`, `nucleo_familiare.membro1_data_nascita`), `date_of_<x>` or `<x>_date` (e.g. `identity.date_of_birth`) must be a real calendar date; hyphens count as underscores, and names like `date_format` are not treated as dates. Both `YYYY-MM-DD` and the Italian `DD/MM/YYYY` (also `DD-MM-YYYY`, `DD.MM.YYYY`) are accepted and stored as `YYYY-MM-DD`; impossible dates such as `31/02/2027` are rejected.

`pvault expiring [days]` lists every `*data_scadenza` field (permesso di soggiorno, passaporto, CIE, patente, tessera sanitaria, ...) that has expired or expires within the window (default 90 days), soonest first.

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

## Service Tokens

Service tokens let agents authenticate with scoped, time-limited, sensitivity-capped credentials. They follow the 1Password service account pattern.

```sh
pvault create-service-token tax-agent --scope "identity.*,financial.*" --ttl 1h
pvault create-service-token checkout --scope "payment.*" --max-sensitivity critical --ttl 1h
pvault list-service-tokens
pvault revoke-service-token 3f9a1c2b7d4e...   # token_prefix from the list, or the full token
```

- `--scope` is required (`"*"` must be passed explicitly).
- `--ttl` defaults to `24h`, maximum `2160h` (90 days).
- `--max-sensitivity` defaults to `sensitive`; `critical` fields are hidden unless you opt in.
- Service tokens cannot change sensitivity tiers.
- `--access` defaults to `read` (read only). `append` also allows adding new fields and documents but never overwriting or deleting them; `write` allows everything. Tokens created before this option existed are read-only.
- Service tokens do **not** reset the 30-minute auto-lock timer; only your session does.

## HTTP API

The vault runs at `http://127.0.0.1:7200`. All protected endpoints require `Authorization: Bearer <token>`.

### Public

```
GET  /vault/status                       # { initialized, locked, field_count, categories }
GET  /vault/schema                       # Recommended field names and sensitivity tiers
POST /vault/unlock                       # { password, secret_key } → { token }
POST /vault/session                      # { password, secret_key?, ttl? } → { token, expires_at } (unlocked vault only, max 1h)
```

### Fields

```
GET    /vault/fields                     # List all field metadata (no values)
GET    /vault/fields/{id}                # Get field with decrypted value
PUT    /vault/fields/{id}                # { value, sensitivity? } — upsert
DELETE /vault/fields/{id}                # Delete field
GET    /vault/fields/category/{name}     # All fields in category with values
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
      { "id": "identity.full_name", "category": "identity", "field_name": "full_name", "value": "Cool Cucumber", "sensitivity": "standard" }
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
GET    /vault/tokens/service             # List active tokens (hash prefixes only)
DELETE /vault/tokens/service/{ref}       # Revoke by listed hash prefix (≥8 chars) or full token
```

### Session

```
POST /vault/lock                         # Lock vault, zero keys
```

### Audit

```
GET /vault/audit?limit=50                # Recent access log
```

## Security Model

```
Profile Password + Secret Key (128-bit)
  → Argon2id KDF (64MB, 3 iterations)
  → Vault Key (256-bit, in-memory only)
  → HKDF per category → Category Subkeys
  → AES-256-GCM per field (12-byte random nonce)
```

- Profile password is never stored
- Secret key lives at `~/.pvault/secret.key` (mode 0600), never transmitted
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
└── pvault.pid     # PID of running server
```
