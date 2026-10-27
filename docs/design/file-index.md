# File index

Status: built, for folders on disk (including the sync folders of OneDrive, SharePoint, Google
Drive and Dropbox): the store and vault layers, the API, `pvault files`, the local sync
(`internal/filesync`) and the MCP tools (`vault_files_search`, `vault_file_get`,
`vault_file_suggest`) with the pratica skill's step 0.4 using them. Not built:
retention/`Expiring` entries and the cloud API connectors (see the end).

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

## Still to do

1. Microsoft Graph (OneDrive, SharePoint), then Google Drive and Dropbox, as read-only
   clients with refresh tokens in the Keychain: sources without a folder, change feeds, and
   matching an API item to the folder entry of the same file (account + path). Open: who owns
   the OAuth client IDs (Google's restricted scopes, Microsoft tenant-admin consent).
2. An unattended sync (a launchd job with a `files.*` write token in the Keychain).
