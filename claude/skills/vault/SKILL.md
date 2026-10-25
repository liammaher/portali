---
name: vault
description: Read or change the user's encrypted personal vault (pvault) - Italian bureaucracy data like residenza, domicilio, codice fiscale, CIE, permesso di soggiorno, tessera sanitaria, plus stored documents. Use when the user asks to open, check, look up, update or fix anything in "my vault", or needs personal details for a form or government portal.
---

# Personal vault (pvault)

Local encrypted vault. Server on `http://127.0.0.1:7200`, data in `~/.pvault/`.
Source: https://github.com/liammaher/personal-vault-italia (see its CLAUDE.md for internals).

## Hard rules

- Never type, ask for, or echo the profile password, secret key, or any token value.
- Never read `~/.pvault/secret.key` or `~/.pvault/vault.db` directly.
- Never create service tokens yourself; the user runs `pvault create-service-token`.
- Never put tokens in URLs or command lines you show the user.
- Fields with tier `personal` (SPID/CIE credentials) are invisible to tokens on purpose.
  Send the user to the UI for those; don't try to work around the 404/403.
- Never open the vault UI in a browser you control (the built-in browser pane or
  Claude in Chrome). Signed in, that page has full access to every field.
- Never read the `pvault-mcp` Keychain item or anything in `~/.pvault/` yourself
  (permission rules deny it too). Use the MCP tools, which enforce the token's limits.

## 1. Check state (no auth needed)

```sh
curl -s http://127.0.0.1:7200/vault/status
```

- Connection refused: the server isn't running. Ask the user to run `pvault unlock`
  in their own terminal (it prompts for the password).
- `"locked":true`: same, `pvault unlock`.
- `"locked":false`: ready. `categories` shows how many fields each category has.

## 2. Pick a path to read or change data

**a. Vault MCP tools (preferred when loaded).** `vault_get`, `vault_set`, `vault_list`,
`vault_context`, `vault_expiring`, `vault_documents`, `vault_document_add`,
`vault_document_get`. They use a scoped service token, so a call can fail with
"scope denied", 403 or 404 when the field is outside the token's scope, access level
or sensitivity cap. Then tell the user which scope/access is missing and fall back to b.
Before `vault_set` overwrites an existing value, show old and new value and confirm.

**b. Web UI, in the user's own browser.** Run `pvault ui` from Bash: it opens
`http://127.0.0.1:7200/ui` in their default browser. The user types their password
into the page; access lasts 15 minutes in that tab ("Session expired" means reload
and sign in again). Tell them which section and field to edit. Don't look at or
read that page; ask the user to confirm when they're done.

**c. CLI, run by the user.** CLI commands that touch data prompt for the password and
need an interactive TTY, so they fail from Claude's Bash. Give the user the exact
command instead, e.g. `pvault set residenza.data_inizio 2026-03-15`.
Commands that work from Bash: `pvault status`, `pvault schema`, `pvault ui`.

## 3. Field IDs and formats

Field IDs are `category.field`. Full list with descriptions and default tiers:

```sh
curl -s http://127.0.0.1:7200/vault/schema
```

Common ones: `residenza.{indirizzo,civico,cap,comune,provincia,data_inizio}`,
`domicilio.*` (only if different from residenza), `identity.codice_fiscale`,
`cie.{numero,data_scadenza}`, `permesso_soggiorno.{numero,tipo,data_scadenza}`,
`tessera_sanitaria.{numero,data_scadenza,medico_base}`.

Validation done by the vault (expect errors otherwise):
- Fields named `data_*`, `date_of_*` or `*_date`: real dates as `YYYY-MM-DD`.
- `cap`: 5 digits. `provincia`: 2-letter sigla (e.g. `NA`).
- Writing without a tier keeps the existing tier; don't pass one unless asked.

## 4. Granting MCP access (per task, on demand)

The MCP server is registered user-wide as `vault` with `VAULT_KEYCHAIN_SERVICE=pvault-mcp`.
It reads its token from the macOS Keychain on every request, so a new or replaced token
works immediately. No restart needed. Creating a new token with `--keychain pvault-mcp`
revokes the one it replaces.

No token is kept around between tasks. When a task needs vault data and a tool says
"no service token" or "invalid, expired or revoked", give the user one command,
scoped to just that task, for them to run in their own terminal (it asks for their
password and never prints the token):

```sh
pvault create-service-token claude --scope "residenza.*" --access write --ttl 2h --keychain pvault-mcp
```

- Scope: only the categories the task touches (e.g. `"identity.*,residenza.*"`).
- Access: `read` if you only need to look things up; `write` only to change fields.
- TTL: short, e.g. `2h`.

After they say it's done, retry the tool call. Running the command again replaces
(and revokes) the stored token. Tokens are listed with `pvault list-service-tokens` and revoked
with `pvault revoke-service-token <prefix>`.

Access levels: `read` (default), `append` (add new fields/documents, never overwrite),
`write` (overwrite and delete). Default sensitivity cap is `sensitive`; suggest
`--max-sensitivity critical` only if the user explicitly wants that.
