# File index

Status: built, for folders on disk (including the sync folders of OneDrive, SharePoint, Google
Drive and Dropbox): the store and vault layers, the API, `pvault files`, the local sync
(`internal/filesync`) and the MCP tools (`vault_files_search`, `vault_file_get`,
`vault_file_suggest`) with the pratica skill's step 0.4 using them. Also built: `files_closed` entries for linked client ID files, and read-only connectors for
OneDrive, SharePoint, Google Drive and Dropbox through their APIs (`internal/filesync`: OAuth
with PKCE on a loopback port, tokens in the Keychain, full listing per sync). The connectors
have been tested against fake servers only (see the end).

## What it is for

The vault keeps encrypted *copies* of scans (`pvault doc add`). The files people work with
(contracts, visure, receipts, client folders) are elsewhere: on disk and in OneDrive,
SharePoint, Google Drive or Dropbox. The index records where each is and whom it concerns
(the owner, a family member, the business, a client, a pratica), so "which files do we
have for Rossi's TARI?" has one answer wherever the files sit. It holds metadata and links
only; a file's bytes and text never enter the vault.

## Decisions

- **D9. New tables, not `vault_documents` and not fields.** A document row assumes an
  encrypted file in `documents/`: backup drops rows without one, restore fails verification
  on them, export reports them missing. Fields (`fileref.<slug>_titolo` ...) fit a few
  hundred curated links, not a crawled index: ten fields a file would fill `pvault list`,
  export and the household view, and force a `schema.json` bump and a library change for a
  different kind of data. The tables live in `vault.db`, so backups carry them with no
  extra files.
- **D10. The vault never goes online.** Connectors are a client (`pvault files sync`) that
  reads a source and writes the index through the API. Cloud tokens stay in the Keychain
  and never reach the vault server, the MCP server or an agent.
- **D11. Read-only.** No code writes to, moves, deletes or shares anything at a source. A
  sharing link is a bearer credential and is never stored (`CheckFileURL`).
- **D12. Nothing is deleted on its own.** A file gone from its source is tombstoned
  (`gone_at`) with its links; `PruneFiles` removes only tombstones with no link, when the
  user asks. A file link to a client's ID or procura scan is a reference: the vault cannot
  erase the file, and the retention report must say so.
- **Opaque by default.** Names, paths, URLs, sizes, mtimes and hashes are encrypted
  (info `files/index`, AAD `<kind>:<row id>`, so a blob copied onto another row fails).
  The plain columns are the holder's slug, the link kind, the pratica's slug and tiers,
  all already plain elsewhere. Search decrypts in memory; there is no plaintext index and
  no plaintext hash column (it would leak equality).

## Who sees what

Scope IDs are `files.index` (owner and business) and `leo:files.index`, so `files.*`,
`leo:files.*` and `*:files.*` work in service-token scopes. A file with no link is the
owner's; a file with links is visible through any link whose holder the caller reaches.
A name or path with a login-secret word (the `IsCredentialField` words) is stored
`personal` and no token sees it. Linking, rules and accepting suggestions are the user's
(session or an approved grant); a service token can only suggest.

## Review findings that shaped it

An independent review of the first version found, and these fixes cover: grants checked only
for "approved in person", not for write access or the person's scope; a sync token able to
rewrite or tombstone files it could not see; a file whose last person link went becoming the
owner's (a `scoped` mark now keeps it out of the owner's scope); a pratica-slug probe through
suggestions; credential words missed on long paths; a stale cache after an outside write;
a link coming back after the user removed it; batches that a single odd name could fail; and
sync work quadratic in the index size. A path keeps its spaces (so it matches its key).
Not done: Unicode normalisation (an NFD folder name from an old volume won't match an NFC
rule; `golang.org/x/text` isn't a dependency), and audit rows name no token.

## How a folder is synced

`pvault files sync` walks each folder source (no links followed, hidden entries, `~$` lock
files and odd names skipped) and sends the files in batches of 200 under one sync ID. The
vault remembers the keys each batch named, in memory (and the source's decrypted files, so a
batch doesn't decrypt them all again); `sync/finish` then marks the rest gone. Only the first
batch (`begin`) starts a sync. If the vault locks, restarts or prunes mid-sync, or another sync
of the source begins, the later batches and the finish are refused (`ErrSyncExpired`): carrying
on would let the finish mark files gone that only the lost batches named. The client never calls
finish after an incomplete scan (an unreadable folder, a disconnected drive) or when a source
that has files comes back empty. A local file's key is its path below the source folder, so a
rename shows as a new file plus a gone one: the old entry keeps its links, a folder rule links
the new one. On macOS an online-only file (the `SF_DATALESS` flag; verify against a real
OneDrive/Drive/Dropbox folder) is counted and indexed from `stat` alone. File hashes are not
computed yet.

## Reading an account through its API

An API source has no `root`; the vault holds its OAuth client ID, tenant, drive and account
name (`FileSourceConfig`), never a token. `pvault files source connect` signs in (authorization
code + PKCE, loopback redirect, state checked), creates the source, then saves the tokens in
the Keychain item `pvault-files-<id>`; if that fails, the source is taken back. A sync opens
the provider with that item, refreshes the access token when it is within a minute of its end
and writes a rotated refresh token back before using it, then lists the whole account and
sends it through the same begin/batches/finish as a folder. The token goes only to the
provider's own host (a "next page" link naming another host is not followed). 401 refreshes
once, 429/503 wait the provider's `Retry-After`, any other failure leaves the scan
incomplete, so nothing is marked gone. Scopes are the read-only ones (Graph `Files.Read`,
`Files.Read.All`+`Sites.Read.All` for SharePoint; Google `drive.metadata.readonly`; Dropbox
`files.metadata.read`). Graph delta never says a path, so paths are built from parent IDs;
Drive's are built from `parents`, with files whose parent the user can't see under
`/Shared with me`; Dropbox gives full paths.

## Files found through another connector

An agent with Claude's own Drive connector can `vault_file_register` a file: provider, the
provider's file id, name, path, page address, and optionally whom to link it to. The vault
queues it (`vault_file_proposals`, encrypted, bounded, one per provider+key); only the user's
`pvault files proposals accept` indexes it, then in a source with `via: connector`: no folder,
no sign-in, never synced, so nothing marks it gone and nothing refreshes it. The proposal
is untrusted (names and words through a model), so the web address must be on the provider's own
site (it is what `pvault files open` would open), a sharing link is dropped, and a token can
propose only for the holders and pratiche its scope reaches.

## Still to do

1. Change feeds (Graph delta tokens, Drive changes, Dropbox cursors) so a sync doesn't list
   the whole account, and matching an API item to the folder entry of the same file
   (account + path): an account read both ways is indexed twice.
2. Testing the connectors against live accounts.
3. An unattended sync (a launchd job with a `files.*` write token in the Keychain).
