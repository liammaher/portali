<h1 align="center">Personal Vault — Italia</h1>

**A local, encrypted vault for the personal data Italian bureaucracy keeps asking for — codice fiscale, CIE, residenza, permesso di soggiorno, document scans — with scoped, read-only-by-default access for AI agents that fill in forms for you.**

---

You enter your details and scan your documents once. Agents that prepare forms (TARI, residenza, INPS, Agenzia delle Entrate…) read exactly the fields you allow, for as long as you allow, and never see your SPID or CIE credentials.

- **Local-first.** One SQLite database and an encrypted `documents/` folder in `~/.pvault/`. No cloud, no account.
- **Encrypted per field and per file.** Argon2id → AES-256-GCM with per-category keys; filenames are encrypted too.
- **Italian-aware.** Validates codice fiscale (incl. omocodia), partita IVA, CIE number, CAP and provincia; normalizes dates; tracks document expiry.
- **Agent-safe by design.** Service tokens are scoped, short-lived, capped by sensitivity, and **read-only unless you say otherwise**.
- **A personal tier only you can see.** SPID login, CIE PIN/PUK and similar secrets are stored as `personal`: no agent token, whatever its scope or cap, can read, list, change or even detect them.

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

You need both the profile password and the secret key to unlock.

For secrets (SPID password, CIE PIN/PUK), run `pvault set <id>` **without** a value: you'll type it hidden, so it never lands in your shell history. `pvault export` leaves personal fields out unless you add `--include-personal`. While unlocked, every CLI command that reads or changes data asks for your password again (interactive terminal only), so an agent running shell commands can't use your session.

## What it stores

| Category | Fields (examples) |
|---|---|
| `identity` | name, `codice_fiscale` (validated), date/place of birth, sex, citizenship |
| `cie` | `numero` (CA12345AB, validated), issue/expiry date, issuing comune |
| `residenza` / `domicilio` | `indirizzo` (via/piazza), `civico`, `interno` (flat number), `cap` (validated), `comune`, `provincia` (validated), `data_inizio` (residenza only) |
| `abitazione` | `titolo` (locatario, proprietario…), catasto `foglio`/`particella`/`subalterno`, `catasto_categoria` (A/2, validated), rendita, `superficie_calpestabile_mq`, APE class |
| `locazione` | contract type, start/end dates (end tracked by `pvault expiring`), rent and deposit (sensitive), cedolare secca, RLI registration details, landlord's name, `locatore_codice_fiscale` and `locatore_iban` (validated) |
| `utenze` | `luce_pod` and `gas_pdr` (validated), supplier and customer code per utility |
| `tari` | `codice_contribuente`, declaration date and protocollo, taxed area and occupants |
| contacts & bank | `identity.phone` (+39…), `identity.pec`, `financial.iban` (validated) |
| `nucleo_familiare` | members as `membro<N>_nome`, `membro<N>_codice_fiscale`, … |
| `business` | `partita_iva` (validated), PEC, ATECO, regime fiscale, REA |
| `spid` (personal) | provider, livello, username, password, renewal date — plus `cie.pin` / `cie.puk` |
| `permesso_soggiorno` | numero, tipo, questura, issue/expiry date |
| `passaporto`, `patente`, `tessera_sanitaria` | numero, issuing authority/country, expiry date |

`pvault schema` lists every recommended field. You can add any `category.field` of your own.

```sh
pvault set identity.codice_fiscale "RSSMRA85T10A562S"
pvault set permesso_soggiorno.data_scadenza 15/12/2026     # stored as 2026-12-15
pvault set spid.password                                   # prompts hidden; stored as personal — only you can read it
pvault set-sensitivity spid.password sensitive             # refused — credentials can only be personal
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
| `--max-sensitivity` | `sensitive` | `critical` fields (e.g. payment cards) stay hidden unless you opt in; `personal` is never available |
| `--ttl` | `24h` | maximum 90 days |
| `--keychain` | — | macOS: save the token in your Keychain under this service name instead of printing it, revoking the token it replaces |

Agent activity never keeps the vault unlocked: it auto-locks after 30 minutes without *your* activity.

### MCP server (Claude and other MCP clients)

Build it from this repo (the `personal-vault-mcp` package on npm is unrelated; don't install it):

```sh
cd mcp && npm ci && npm run build
```

```json
{
  "mcpServers": {
    "vault": {
      "command": "node",
      "args": ["/path/to/personal-vault-italia/mcp/dist/index.js"],
      "env": { "VAULT_KEYCHAIN_SERVICE": "pvault-mcp" }
    }
  }
}
```

With that config, `pvault create-service-token claude --scope "residenza.*" --ttl 2h --keychain pvault-mcp` grants access mid-session, with no restart and no token on screen. Outside macOS, set `"VAULT_TOKEN": "<token>"` instead.

Tools: `vault_get`, `vault_list`, `vault_context`, `vault_set`, `vault_schema`, `vault_status`, `vault_expiring`, `vault_documents`, `vault_document_get`, `vault_document_add`. The MCP server only ever uses the token you give it, and `vault_document_add` only reads files from `~/Downloads` (configurable with `VAULT_IMPORT_DIRS`). See [mcp/README.md](mcp/README.md).

### Claude Code setup

[`claude/`](claude/) has optional pieces that make Claude Code use the vault safely:

- **Vault skill** ([`claude/skills/vault/SKILL.md`](claude/skills/vault/SKILL.md)): how to check the vault, read or change fields through the MCP tools, ask for a narrow short-lived token when it needs one, and send you to the web UI in your own browser rather than one Claude controls. Install it for all projects:

  ```sh
  mkdir -p ~/.claude/skills && cp -R claude/skills/vault ~/.claude/skills/
  ```

- **Pratica skill** ([`claude/skills/pratica/SKILL.md`](claude/skills/pratica/SKILL.md)): runs an Italian procedure end to end (research, gap check against the vault, filling the form or portal, handing you the login, signature, payment and sending, then tracking the reply). Each procedure gets a playbook in [`claude/skills/pratica/playbooks/`](claude/skills/pratica/playbooks/) after its first run, so later runs follow it. Playbooks hold field IDs, never your data. Open procedures are tracked in the vault as `pratiche.<slug>_<campo>` fields, and their deadlines show in `pvault expiring`. Link it rather than copying it, so new playbooks land in this repo. Run this from your main checkout, not a temporary git worktree, or the link breaks when the worktree is removed:

  ```sh
  ln -s "$PWD/claude/skills/pratica" ~/.claude/skills/pratica
  ```

- **Permission rules** ([`claude/settings.example.json`](claude/settings.example.json)): deny rules that stop Claude's own tools from reading `~/.pvault/` or Keychain passwords (including the MCP token), so it goes through the MCP's scope and sensitivity checks. Merge the `permissions.deny` list into `~/.claude/settings.json`; don't replace the file.

The rules match command text, so they stop direct or accidental access, not a determined attempt (a script that reads the Keychain internally isn't caught). For a hard boundary, also enable Claude Code's sandbox with `~/.pvault` in `sandbox.filesystem.denyRead`.

Whatever a token allows, Claude sees: values returned by the MCP tools enter the conversation like any other text. What it never sees is your password, your secret key, `personal`-tier items (SPID/CIE credentials), and anything outside the token's scope or after it expires.

## Security model

```
Profile password + 128-bit secret key
  → Argon2id (64 MB, 3 iterations) → vault key (memory only, zeroed on lock)
  → HKDF per category → AES-256-GCM per field, and per document (bound to its ID)
```

- The server listens on 127.0.0.1 only and rejects non-loopback `Host` headers (DNS rebinding).
- No session token is ever written to disk; the CLI and onboarding page get short-lived grants by re-entering the password. Wrong passwords are rate-limited.
- Service tokens are stored hashed; scope, sensitivity cap and access level are enforced server-side.
- Sensitivity tiers: `public` < `standard` < `sensitive` < `critical` < `personal`. Agent tokens can be capped up to `critical`; `personal` is reachable only by your own session. SPID/CIE credentials (under any field name) can only be stored as `personal`, and SPID/CieID categories default to it.

**What it can't protect against:** malware or an agent running as your macOS user can still stop the vault, read `~/.pvault/secret.key` (useless without your password), or tamper with the `pvault` binary to capture your password. Keep the vault locked when you're not using it.

**On automating government portals:** the vault prepares data; it doesn't log in for you. SPID and CIE credentials are strictly personal — authenticate yourself, and use each portal's official delegation (e.g. Agenzia delle Entrate intermediaries) for anything done on someone else's behalf.

## More

- [docs/usage.md](docs/usage.md) — full CLI and HTTP API reference
- [mcp/README.md](mcp/README.md) — MCP server setup and errors
- `make test` — run the test suite (race detector on)

## License

MIT — see [LICENSE](LICENSE). Original work © Cyrus Goh ([lovincyrus/personal-vault](https://github.com/lovincyrus/personal-vault)); Italian extensions and security changes © Liam Maher.
