<h1 align="center">Personal Vault — Italia</h1>

**A local, encrypted vault for the personal data Italian bureaucracy keeps asking for — codice fiscale, CIE, residenza, permesso di soggiorno, document scans — with scoped, read-only-by-default access for AI agents that fill in forms for you.**

---

You enter your details and scan your documents once. Agents that prepare forms (TARI, residenza, INPS, Agenzia delle Entrate…) read exactly the fields you allow, for as long as you allow, and never see your SPID or CIE credentials.

- **Local-first.** One SQLite database and an encrypted `documents/` folder in `~/.pvault/`. No cloud, no account.
- **Encrypted per field and per file.** Argon2id → AES-256-GCM with per-category keys; filenames are encrypted too.
- **Italian-aware.** Validates codice fiscale (incl. omocodia), partita IVA, CIE number, CAP and provincia; normalizes dates; tracks document expiry.
- **Agent-safe by design.** Service tokens are scoped, short-lived, capped by sensitivity, and **read-only unless you say otherwise**. SPID credentials and CIE PIN/PUK are refused outright.

> Fork of [lovincyrus/personal-vault](https://github.com/lovincyrus/personal-vault) (MIT), extended with the Italian schema, document storage and a hardened agent access model.

## Quick start

Requires Go 1.26+.

```sh
git clone https://github.com/liammaher/personal-vault-italia.git
cd personal-vault-italia
make build                      # → bin/pvault
ln -s "$PWD/bin/pvault" /opt/homebrew/bin/pvault   # or: sudo make install

pvault init      # choose a profile password; save the secret key it prints
pvault unlock    # starts the local server on 127.0.0.1:7200
pvault ui        # opens the onboarding form in your browser
```

You need both the profile password and the secret key to unlock. While unlocked, every CLI command that reads or changes data asks for your password again (interactive terminal only), so an agent running shell commands can't use your session.

## What it stores

| Category | Fields (examples) |
|---|---|
| `identity` | name, `codice_fiscale` (validated), date/place of birth, sex, citizenship |
| `cie` | `numero` (CA12345AB, validated), issue/expiry date, issuing comune |
| `residenza` / `domicilio` | via, civico, CAP, comune, provincia |
| `nucleo_familiare` | members as `membro<N>_nome`, `membro<N>_codice_fiscale`, … |
| `business` | `partita_iva` (validated), PEC, ATECO, regime fiscale, REA |
| `permesso_soggiorno` | numero, tipo, questura, issue/expiry date |
| `passaporto`, `patente`, `tessera_sanitaria` | numero, issuing authority/country, expiry date |

`pvault schema` lists every recommended field. You can add any `category.field` of your own.

```sh
pvault set identity.codice_fiscale "RSSMRA85T10A562S"
pvault set permesso_soggiorno.data_scadenza 15/12/2026     # stored as 2026-12-15
pvault set spid.password hunter2                           # refused — logins never go in the vault
```

## Document scans

```sh
pvault doc add permesso_soggiorno ~/Desktop/permesso.pdf   # encrypted, max 20 MB
pvault doc list
pvault doc get <id> --out /tmp/permesso.pdf                # decrypted copy, owner-only, never overwrites
pvault doc delete <id>
```

After importing, delete or securely archive the unencrypted originals.

## Expiry tracking

```sh
pvault expiring          # expired or expiring in the next 90 days
pvault expiring 365
```

```
passaporto           2026-09-30  EXPIRED 24 days ago
permesso_soggiorno   2026-12-15  in 52 days
```

## Giving an agent access

Create a token for each agent, scoped to what it needs:

```sh
pvault create-service-token tari-agent --scope "identity.*,residenza.*" --ttl 2h
pvault create-service-token archiver   --scope "residenza.*" --access append
pvault list-service-tokens
pvault revoke-service-token <token_prefix>
```

| Option | Default | Meaning |
|---|---|---|
| `--scope` | required | `category.*`, exact `category.field`, `category.documents`, or `*` |
| `--access` | `read` | `append` = may add new fields/documents but never overwrite or delete; `write` = full |
| `--max-sensitivity` | `sensitive` | `critical` fields (e.g. payment cards) stay hidden unless you opt in |
| `--ttl` | `24h` | maximum 90 days |

Agent activity never keeps the vault unlocked: it auto-locks after 30 minutes without *your* activity.

### MCP server (Claude and other MCP clients)

```sh
cd mcp && npm install
```

```json
{
  "mcpServers": {
    "vault": {
      "command": "npx",
      "args": ["tsx", "/path/to/personal-vault-italia/mcp/src/index.ts"],
      "env": { "VAULT_TOKEN": "<token from pvault create-service-token>" }
    }
  }
}
```

Tools: `vault_get`, `vault_list`, `vault_context`, `vault_set`, `vault_schema`, `vault_status`, `vault_expiring`, `vault_documents`, `vault_document_get`, `vault_document_add`. The MCP server only ever uses the token you give it, and `vault_document_add` only reads files from `~/Downloads` (configurable with `VAULT_IMPORT_DIRS`). See [mcp/README.md](mcp/README.md).

## Security model

```
Profile password + 128-bit secret key
  → Argon2id (64 MB, 3 iterations) → vault key (memory only, zeroed on lock)
  → HKDF per category → AES-256-GCM per field, and per document (bound to its ID)
```

- The server listens on 127.0.0.1 only and rejects non-loopback `Host` headers (DNS rebinding).
- No session token is ever written to disk; the CLI and onboarding page get short-lived grants by re-entering the password. Wrong passwords are rate-limited.
- Service tokens are stored hashed; scope, sensitivity cap and access level are enforced server-side.
- SPID credentials and CIE PIN/PUK are rejected under any field name.

**What it can't protect against:** malware or an agent running as your macOS user can still stop the vault, read `~/.pvault/secret.key` (useless without your password), or tamper with the `pvault` binary to capture your password. Keep the vault locked when you're not using it.

**On automating government portals:** the vault prepares data; it doesn't log in for you. SPID and CIE credentials are strictly personal — authenticate yourself, and use each portal's official delegation (e.g. Agenzia delle Entrate intermediaries) for anything done on someone else's behalf.

## More

- [docs/usage.md](docs/usage.md) — full CLI and HTTP API reference
- [mcp/README.md](mcp/README.md) — MCP server setup and errors
- `make test` — run the test suite (race detector on)

## License

MIT — see [LICENSE](LICENSE). Original work © Cyrus Goh ([lovincyrus/personal-vault](https://github.com/lovincyrus/personal-vault)); Italian extensions and security changes © Liam Maher.
