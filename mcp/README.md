# Vault MCP Server

MCP server that exposes your encrypted personal vault to AI agents.

## Install

Build it from this repo. It isn't published to npm, and the unrelated
`personal-vault-mcp` package there is someone else's code, so don't install
or `npx` it: it would run with your vault token.

```sh
cd mcp
npm ci
npm run build
```

## Register with Claude Code

On macOS, keep the token in your Keychain (recommended). Register once:

```sh
claude mcp add vault --scope user \
  -e VAULT_KEYCHAIN_SERVICE=pvault-mcp \
  -- node /absolute/path/to/personal-vault-italia/mcp/dist/index.js
```

Then, whenever an agent needs access, create a token straight into the Keychain.
It's never printed, and the running server picks it up on its next call, so
there's no need to restart Claude. Creating a new one revokes the token it
replaces, so only one Keychain token is ever valid:

```sh
pvault create-service-token claude --scope "residenza.*" --access write --ttl 2h --keychain pvault-mcp
```

Elsewhere, pass the token directly (stored in plain text in `~/.claude.json`):

```sh
claude mcp add vault --scope user -e VAULT_TOKEN=<token> -- node /absolute/path/to/personal-vault-italia/mcp/dist/index.js
```

## Tools

| Tool | Description |
|------|-------------|
| `vault_status` | Check if the vault is running and unlocked |
| `vault_get` | Get a single decrypted field by ID |
| `vault_list` | List all field metadata (no values) |
| `vault_context` | Get all decrypted fields grouped by category |
| `vault_set` | Set an encrypted field value in the vault |
| `vault_expiring` | Documents expired or expiring within N days (default 90) |
| `vault_documents` | List encrypted document scans the token may access |
| `vault_document_add` | Encrypt and archive a local file (e.g. a downloaded receipt) |
| `vault_document_get` | Read a stored document (images inline, others as an embedded resource, max 10 MB) |

Tokens are read-only unless created with `--access append` (also add new fields and documents, never overwrite or delete) or `--access write` (full). `vault_set` and `vault_document_add` need append or write.

With PEC set up (see [PEC mailbox](#pec-mailbox-read-only)), five more tools appear:

| Tool | Description |
|------|-------------|
| `pec_folders` | List the PEC mailbox's folders |
| `pec_list` | Newest-first messages, filterable by sender, subject, text, date or unread; each tagged with its PEC `kind` |
| `pec_read` | One message, unwrapped from the PEC envelope, with its text, attachment list and the provider's `daticert.xml` record |
| `pec_attachment` | One attachment (images inline, text as text, PDFs and the rest as an embedded resource, max 10 MB) |
| `pec_save_attachment` | Save one attachment into the first `VAULT_IMPORT_DIRS` folder (owner-only, never overwrites) so `vault_document_add` can archive it |

Documents follow the same token rules as fields: scope pattern `<category>.documents` (or `<category>.*`) and the token's sensitivity cap (documents default to `sensitive`).

## PEC mailbox (read-only)

The server can read your PEC mailbox over IMAP so an agent can find notices from the Comune, Agenzia delle Entrate and others, and check receipts for PEC you sent. It can't send: there is no SMTP code at all, because a PEC message has the legal weight of a registered letter. Folders are opened with `EXAMINE` and bodies fetched with `BODY.PEEK`, so nothing is marked read, moved or deleted. The only thing written locally is a file `pec_save_attachment` creates in your import folder.

1. In your provider's account panel, turn on 2-step verification and generate a password for mail programs. On Aruba: account.aruba.it/pec, Verifica in 2 passaggi. It works only over IMAP/SMTP (not webmail), can be revoked there, and Aruba shows it once.
2. Store it in your Keychain. The command prompts for it, so it never lands in shell history or a config file:

   ```sh
   security add-generic-password -U -a you@pec.it -s pvault-pec -w
   ```

3. Add the PEC settings to the server's registration:

   ```sh
   claude mcp remove vault --scope user
   claude mcp add vault --scope user \
     -e VAULT_KEYCHAIN_SERVICE=pvault-mcp \
     -e PEC_ADDRESS=you@pec.it -e PEC_KEYCHAIN_SERVICE=pvault-pec \
     -- node /absolute/path/to/personal-vault-italia/mcp/dist/index.js
   ```

The password is read from the Keychain for every call, so when the app password expires (Aruba's reportedly last 90 days) you only rerun step 2.

What `kind` means in `pec_list` and `pec_read`:

| `kind` | Meaning |
|--------|---------|
| `posta-certificata` | A certified message someone sent you; `pec_read` returns the original from `postacert.eml` |
| `ricevuta-accettazione`, `ricevuta-avvenuta-consegna`, `ricevuta-errore-consegna`, ... | A receipt for a message you sent |
| `anomalia` | Ordinary or malformed mail that reached the PEC box; not certified |
| `email` | No PEC transport headers |

`certificazione` in `pec_read` is the provider's `daticert.xml`: sender, recipients, subject, date and delivery result. Prefer it over the message headers for who sent what and when. Its S/MIME signature is not verified by this server.

## Authentication

The MCP server only uses a service token you create, from (in order) `VAULT_TOKEN`, the Keychain item named by `VAULT_KEYCHAIN_SERVICE`, or the output of `VAULT_TOKEN_COMMAND`:

```sh
pvault create-service-token claude --scope "identity.*,residenza.*" --ttl 24h [--keychain pvault-mcp]
```

It never uses your own session, which has full, uncapped access and would bypass the scope and sensitivity limits. The server refuses to start when none of these is set.

`VAULT_KEYCHAIN_SERVICE` reads the item `--keychain` wrote, for the same macOS user, with `/usr/bin/security`. Keychain lookups and `VAULT_TOKEN_COMMAND` run for every request (nothing is cached), so a replaced token is used immediately, and their error is included when they fail. Until a token is stored, tools answer with the exact command to create one.

The token keeps working across vault lock/unlock cycles until it expires or is revoked.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `VAULT_ADDR` | `http://127.0.0.1:7200` | Vault server address |
| `VAULT_TOKEN` | — | Service token from `pvault create-service-token` (one token source is required) |
| `VAULT_KEYCHAIN_SERVICE` | — | macOS: Keychain service the token was saved under with `--keychain` (e.g. `pvault-mcp`); used when `VAULT_TOKEN` is unset |
| `VAULT_TOKEN_COMMAND` | — | Command run in the platform shell that prints the token (e.g. another secret store); used when neither of the above is set |
| `VAULT_IMPORT_DIRS` | `~/Downloads` | Colon-separated folders `vault_document_add` may read from (hidden files always refused) |
| `PEC_ADDRESS` | — | Your PEC address (IMAP username). The `pec_*` tools exist only when this is set |
| `PEC_KEYCHAIN_SERVICE` | — | Keychain service holding the PEC app password, stored for account `PEC_ADDRESS` (e.g. `pvault-pec`) |
| `PEC_IMAP_HOST` | `imaps.pec.aruba.it` | IMAP server (TLS) |
| `PEC_IMAP_PORT` | `993` | IMAP port |
| `VAULT_SCOPE` | `*` | Optional extra filter applied in the MCP layer (the server enforces the token's own scope regardless) |

## Error Messages

| Error | Meaning |
|-------|---------|
| `vault: server not running` | Run `pvault unlock` to start the server |
| `vault: no service token` | Create one; the message gives the exact `--keychain` command when using `VAULT_KEYCHAIN_SERVICE` |
| `vault: service token invalid, expired or revoked` | Create a new token (`--keychain` replaces and revokes the stored one) or update `VAULT_TOKEN` |
| `vault: vault is locked` | Run `pvault unlock` to decrypt |
| `vault: this item's sensitivity tier is above ...` | Token's `--max-sensitivity` is too low for this field or document |
| `vault: this is outside the token's scope` | Not covered by the token's `--scope` |
| `vault: this token is read-only` | Create a token with `--access append` or `--access write` |
| `vault: this token can add new items but not overwrite or delete` | Overwriting or deleting needs `--access write` |
| `vault: not found` | Field ID doesn't exist |
| `vault: no service token` | Set `VAULT_TOKEN` in the MCP server config |
| `pec: no password in Keychain service ...` | Store the app password with `security add-generic-password -U -a <address> -s <service> -w` |
| `pec: login to ... failed` | The app password is wrong or expired; generate a new one and store it again |

## Demo

See [examples/shopping-demo](../examples/shopping-demo/) for a demo using Vault MCP + a mock shop.
