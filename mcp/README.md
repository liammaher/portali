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
  -e VAULT_TOKEN_COMMAND='security find-generic-password -a "$USER" -s pvault-mcp -w' \
  -- node /absolute/path/to/personal-vault-italia/mcp/dist/index.js
```

Then, whenever an agent needs access, create a token straight into the Keychain.
It's never printed, and the running server picks it up on its next call, so
there's no need to restart Claude:

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

Documents follow the same token rules as fields: scope pattern `<category>.documents` (or `<category>.*`) and the token's sensitivity cap (documents default to `sensitive`).

## Authentication

The MCP server only uses a service token you create, given either in `VAULT_TOKEN` or printed by `VAULT_TOKEN_COMMAND`:

```sh
pvault create-service-token claude --scope "identity.*,residenza.*" --ttl 24h [--keychain pvault-mcp]
```

It never uses your own session, which has full, uncapped access and would bypass the scope and sensitivity limits. The server refuses to start when neither variable is set.

With `VAULT_TOKEN_COMMAND`, the command runs on the first call that needs a token, and again if the server rejects it (expired, revoked or replaced). Until a token is stored, tools answer with how to create one.

The token keeps working across vault lock/unlock cycles until it expires or is revoked.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `VAULT_ADDR` | `http://127.0.0.1:7200` | Vault server address |
| `VAULT_TOKEN` | — | Service token from `pvault create-service-token` (this or `VAULT_TOKEN_COMMAND` is required) |
| `VAULT_TOKEN_COMMAND` | — | Shell command that prints the token, e.g. a Keychain lookup; ignored when `VAULT_TOKEN` is set |
| `VAULT_IMPORT_DIRS` | `~/Downloads` | Colon-separated folders `vault_document_add` may read from (hidden files always refused) |
| `VAULT_SCOPE` | `*` | Optional extra filter applied in the MCP layer (the server enforces the token's own scope regardless) |

## Error Messages

| Error | Meaning |
|-------|---------|
| `vault: server not running` | Run `pvault unlock` to start the server |
| `vault: no service token` | Create one (with `--keychain` when using `VAULT_TOKEN_COMMAND`) |
| `vault: service token invalid, expired or revoked` | Create a new token (`--keychain` replaces the stored one) or update `VAULT_TOKEN` |
| `vault: vault is locked` | Run `pvault unlock` to decrypt |
| `vault: this item's sensitivity tier is above ...` | Token's `--max-sensitivity` is too low for this field or document |
| `vault: this is outside the token's scope` | Not covered by the token's `--scope` |
| `vault: this token is read-only` | Create a token with `--access append` or `--access write` |
| `vault: this token can add new items but not overwrite or delete` | Overwriting or deleting needs `--access write` |
| `vault: not found` | Field ID doesn't exist |
| `vault: no service token` | Set `VAULT_TOKEN` in the MCP server config |

## Demo

See [examples/shopping-demo](../examples/shopping-demo/) for a demo using Vault MCP + a mock shop.
