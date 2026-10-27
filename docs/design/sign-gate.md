# Sign gate: reviewing a form, then signing it

Status: built (2026-10-27); kept as the record of the decisions. "As built" below lists
where the code differs from the proposal. Decisions are marked **Decision D1, D2, …**, each
with what was chosen at the end.

## Goal

A pratica ends with a form the user must sign (the TARI Mod. 1, for one). Today the skill
hands that over: the agent fills the PDF, the user prints or signs it by hand. Let the
agent ask for a signature instead: the user reviews the exact PDF, approves it in person,
and the vault stamps their handwritten signature on it. Add as little as possible to what
the vault already has: documents, scoped grants, the audit log, the Touch ID prompt.

Not a goal: a general e-signature product (invitations, several signers, sealing,
certificates), a *firma digitale*, or sending the form. All three stay with the user.

## What the signature has to be

Art. 65 CAD makes an istanza sent electronically to a PA valid in any of these cases
([Normattiva](https://www.normattiva.it/uri-res/N2Ls?urn:nir:stato:decreto.legislativo:2005-03-07;82~art65)):
signed in one of the art. 20 forms; the author identified by SPID, CIE or CNS (or formed
through the app IO access point); signed *and sent with a copy of an ID document*; or sent
from the author's own *domicilio digitale* (a PEC). Art. 38 DPR 445/2000 sets the ID-copy
rule for istanze and declarations.

What follows for us:

- **The signature is only one of four ways to be valid.** For a procedure filed from the
  user's own PEC or through an SPID/CIE login, the signature may not be needed at all.
  Some comuni say so (Carbonia: no ID copy if signed digitally or sent by the holder's PEC,
  [form](https://www.comune.carbonia.su.it/wp-content/uploads/2024/06/TARI-Dichiarazione.pdf)).
  Naples's own rule for a new TARI registration was not found; its Mod. 1 still asks for a
  signature and an ID copy, and the library says so (`signature_required: true`).
- **A stamped image is a weak electronic signature.** It is a *firma elettronica semplice*
  (art. 20 CAD: a judge weighs whether it points unmistakably to its author). What makes a filing hold is the ID copy or the PEC
  next to it, not the image. No source found says a PA must accept a pasted image, and some
  comuni may refuse it. Procedures that demand a *firma digitale* (the Comunica filings,
  anything for a business) are outside this design.
- **The user stays legally responsible for what the form declares** (DPR 445 declarations).
  So the approval is the signature in substance; the stamp is its rendering. The design
  puts its weight on the approval, not on the image.

The sources on agents that sign say the same thing from the product side: the agent
prepares, a person completes, and a separate identity and audit trail record who did what
([Cipher Projects](https://www.cipherprojects.com/blog/posts/esignature-mcp-servers-ai-agents-2026/),
[ibl.ai](https://ibl.ai/developer/connectors/docusign/SKILL.md)). They are vendor writing;
none is a legal source on eIDAS and agent-initiated signing.

## Rules

1. **An agent may request, never approve.** No service-token path approves, and no
   scope, access level or tier changes that.
2. **The approved bytes are the signed bytes.** The vault builds the signed PDF first, the
   user reviews that very file, and approving releases it. A change after review is a
   different file with a different hash.
3. **Approval is in person, bound to one request.** It takes a typed password or Touch ID
   and works for that request only, once.
4. **The vault, not the agent, chooses the date** written beside the signature.
5. **Every request, approval and refusal is in the audit log**, with the hashes.

What is *not* protected, said plainly: after the first signed PDF the agent has seen the
signature image (it is in the file the agent gets). The gate stops *new* signatures
without approval; it cannot keep the image secret, as no scanned signature is.

## The vault side

### The signature image

Stored as a document in category `firma_autografa` (not `firma`, which the credential
rules read as a digital-signature word; `firma_digitale` already exists), always tier
`personal`, so a service token cannot list or read it. Exactly one PNG with a transparent
background, up to 500 KB, checked in `AddDocument` for that category (a JPEG or a white
background would paint a box over the signature line); replacing it deletes the old one.
`pvault doc add firma_autografa ~/Desktop/signature.png` stores it. It belongs to the person who
signs: `<slug>:firma_autografa` for a party. v1 signs only for the principal (`io`), as
`fill.py` does today for the intestatario.

### Signing requests (`signing.go`)

A request lives **in memory only**, like grants and for the same reason: nothing about a
pending signature survives a lock, a restart or a backup.

```
POST /vault/signing           service token (scope firma_autografa.signing, access append),
                              or session. Body: the PDF, the form name, the signature
                              placement from the library layout (page, rect, optional date
                              rect), a purpose line. Returns the request id and the
                              candidate's sha256.
GET  /vault/signing           the pending requests: id, name, hash, pages, age, who asked
GET  /vault/signing/{id}      status only: pending | approved | rejected | expired
GET  /vault/signing/{id}/summary   name, pages, short hash, signer. No content. Socket only.
GET  /vault/signing/{id}/candidate  the signed PDF, withheld until approval or a sign grant
POST /vault/signing/{id}/approve    needs the sign grant for {id} and the full hash
DELETE /vault/signing/{id}     reject (session or the requesting token)
```

On `POST`, the vault checks the body is a PDF of at most `MaxDocumentSize`, that the page
and rects exist on it, and that the signer has a signature on file. It decrypts the image
with the document's subkey (`requireUnlocked`, cleared after), stamps it and today's date
at the placement, and holds the resulting file with its sha256. At most 5 pending per
vault, 30 minutes each, on the same calendar day as the stamped date; lock and auto-lock
drop them all. A token sees only the requests it made (`GET` with that token) and never the
candidate.

Stamping must happen here, not in `fill.py`: the vault is the only process that decrypts
the image. **Decision D1.**

### The sign grant

`POST /vault/session` accepts `sign: "<request id>"` (with the password, not with `scope`).
The grant it returns has a new field, `sign`, and can do two things for that request:
read its candidate and approve it. It lasts 10 minutes (long enough to read a form) and is consumed by the approval.

- `ValidateToken` (full access) and `ScopedGrant` (scoped) must both ignore a grant that has
  `sign` set, and a `SigningGrant(token)` method returns only the request id.
  A test per existing path proves a sign grant opens nothing else (fields, documents,
  tokens, backup, `/vault/session` itself).
- A wrong password counts toward the same 10/min limit as any other grant.
- Over TCP the page is the only password client; this grant is socket-only like the CLI's
  (`credentialRequest`).

### The CLI

```
pvault sign list                  pending requests
pvault sign review <id> [--out F] review, approve and write the signed PDF
pvault sign reject <id>
```

`review` is the whole human act, one command run by the user:

1. Ask the vault for the sign grant (password on the TTY, or Touch ID, see below).
2. Fetch the candidate, check its sha256 against the request, write it to a new private
   file (a 0700 directory, 0600 file, random name) and open it in the default viewer.
3. Print the name, pages and the first 8 characters of the hash, and ask the user to type
   those 8 characters to sign. Anything else, or Ctrl-C, signs nothing.
4. Send `approve` with the full hash. The vault compares it with the candidate it holds;
   on a match it marks the request approved, writes the audit entry and the CLI saves the
   file as `<name>-firmato.pdf` (`--out` is a file path; default: the current folder).

The typed characters make approval a step after looking, not a reflex on a fingerprint
prompt. An agent with a pty cannot supply a password or a fingerprint, so it cannot get
past step 1; one that could type step 3 still needs step 1.

### Touch ID (`cmd/pvault-touchid`)

A `sign --id <id>` subcommand. It reads name, pages and hash from `.../summary` itself
over the vault socket (the prompt is never worded from its caller's arguments, as the
scope prompt is parsed, not trusted), then asks for Touch ID with: *sign "<name>" (3
pages, hash ab12cd34), filed by <requester>, with your signature from <vault>, for 10 minutes*, and returns the
sign grant. The wording says "sign", not "read": it never understates. Its prompt is
tested like `grantReason` (`internal/vault/testdata/` gets a signing case file the Go
and Swift sides both read). **Decision D4.**

### Audit

`vault_access_log` already has consumer, scope, action and a purpose line, so no schema
change:

| action | consumer | scope | purpose |
|---|---|---|---|
| `sign_request` | the token's name | `firma_autografa.signing` | `mod-1 3p sha256 ab12…` |
| `sign_approve` | `session` or `grant` | `firma_autografa.signing` | `candidate ab12…` |
| `sign_view` | `session` or `grant` | as above | the file's hash |
| `sign_refused` | as above | as above | a wrong hash, or a stale date |
| `sign_reject` | as above | as above | id |

### MCP (`mcp/src`)

Two tools and no more: `vault_sign_request` (reads a PDF from an import dir, the
way `vault_document_add` does through `resolveImportPath`; sends it with the placement) and
`vault_sign_status`. There is deliberately no approve tool; a test lists the registered
tools and fails if one appears. The service token for a pratica that signs needs
`firma_autografa.signing` with `append`, which the skill's token command adds only when
the layout has a signature.

## The library side (pratiche-napoli)

Changes to `contract/` and the procedures, none of them required for the vault half.

1. **`layout.schema.json`: a `signatures` list.** Each: `rect` (the signature line's box),
   optional `date_rect`, `page` (1-based, default 1; today the layout is page 1 only), and
   `for` (which signer, default the dichiarante). Boxes for fields stay as they are. The
   Mod. 1 layout has none of this yet: someone measures the signature and date lines on the
   current form (the sha256 pins that version).
2. **`procedure.schema.json`: how each channel takes a signature**, per form, because a form
   plus a channel decides it:

   ```yaml
   forms:
     - id: mod-1
       signature_required: true
       signing:
         - channel: pec            # a channel type of this procedure
           ways: [handwritten, digital]
           id_copy: required       # required | not_needed | unknown
           status: unverified      # verified once an ente or a source says so
           source: mod-1
         - channel: portal
           ways: [login]           # the SPID/CIE login is the signature
   ```

   `ways` is a closed list: `handwritten`, `digital`, `login`, `none` (the channel itself
   identifies, art. 65 c-bis). The validator rejects a way no channel can offer
   (`login` needs an `auth` on the channel) and a `signing` for a channel the procedure
   doesn't list. `signature_required` stays; `signing` refines it.
3. **`steps`: the signature step stays `human_only`.** The human act is the approval, so the
   flag keeps its meaning ("never done by an agent"). Add an optional `gate: sign` on the
   step, meaning "the vault's sign gate can carry this step if the way is `handwritten`".
   A consumer without the gate ignores it and hands the step over as today.
4. **Nothing in `schema.json`.** `firma_autografa` is a document-only category like the scan
   categories, which the schema doesn't list; the vault enforces its rules in `AddDocument`.

## The skill and the pilot (this repo)

- **Pratica skill, step 5** ("signatures (handwritten or firma digitale)") becomes: read the
  form's `signing` for the channel the user picked. `none` or `login`: no signature step,
  say why. `handwritten` and a gate with a signature on file: file a request, give the
  user `pvault sign review <id>`. `digital`: the user's, as now. Nothing in the library:
  hand-signing. `status: unverified` for the channel: say that, and let the user choose the
  gate or hand-signing (as built; the first draft fell back to hand-signing outright).
- **`fill.py`** gains `--sign`: after a passing, non-draft run it files the request with the
  layout's `signatures` and prints the id. `prossimo_passo` reads "Rivedere e firmare:
  `pvault sign review <id>`, poi inviare". The intestatario check that exits for anyone
  but `io` stays.
- **The ID copy** is a separate file the vault already holds (a `cie` or `passaporto`
  document); the skill lists the paths with the signed PDF as it lists attachments today.
  Sending remains the user's.
- **Playbooks** (`napoli-tari-iscrizione.md`, "Needs the user"): "Signature" moves from
  always-the-user to "the user, through `pvault sign review` when set up".

## Threats

| What goes wrong | Why it fails |
|---|---|
| An agent files a request for a document the user didn't expect | The user reviews the exact file and types its hash. A request only queues; it expires |
| A prompt-injected agent signs something | It can request, not approve; nothing is signed or released until the user acts |
| A request is approved, then the file swapped | The vault holds the bytes; the hash the user typed is compared to the held file |
| An approval replayed for another request | The grant names one request and is consumed |
| A leaked service token signs | It has no password, no fingerprint, no sign grant |
| Signature image copied out of a signed PDF | Not prevented (see Rules); the gate only stops new stamps, and each is audited |
| A process running as the user reads the viewer's temp file | The limit stated in CLAUDE.md: not a defence against code that already runs as the user |

## Phases

0. **Spike (before any code).** Stamp a PNG and a date at a rect on page N of the real
   Mod. 1 PDF in pure Go, and check the result in Preview and in PyMuPDF. Also settle
   whether Naples takes the TARI Mod. 1 from the holder's own PEC without a signature
   (a question for the Comune, which only the user can ask) and whether the CIE can sign
   (*firma con CIE*), neither of which was verified.
1. **Vault:** the category rules, `signing.go`, the sign grant, endpoints, CLI, audit, tests
   (the table above, one test each, plus lock and expiry), `docs/usage.md`.
2. **Library:** the contract changes and the validator, the Mod. 1 layout and `signing`
   entries (all `unverified` until sourced), then `make schema` sync.
3. **Skill and pilot:** `fill.py --sign`, the MCP tools, `SKILL.md`, the playbook, the
   pilot's regress run with a synthetic signature.
4. **Touch ID:** the `sign` subcommand and its prompt tests. Until then the password route
   works everywhere.
5. **Later:** signing for a party or a client under a procura; a business vault's
   dichiarante (the library's `signer` roles); handing a *firma digitale* provider the
   same candidate for a remote qualified signature (phone OTP, so the user signs).

## As built

- **Stamping** is pdfcpu v0.16.3 (pure Go, no CGO; D1). The spike put an image within 1 pt of
  the requested box on the real Mod. 1 and read back in PyMuPDF; adding it raised `x/crypto`,
  `x/sys` and `x/term`. A rotated or cropped page, an encrypted file or a box off the page is
  refused rather than placed approximately. The date is `DD/MM/YYYY`, in whole points.
- **The vault reads a grant's scope for listing too**: the user's scoped grant (what a Touch
  ID prompt for `firma_autografa.signing` gives `pvault sign list` and `reject`) sees every
  request; a token sees its own.
- **The summary** returns `hash8` (the first 8 characters), not the full hash, as the prompt
  needs; the client computes the full one from the file and the vault compares it on approval.
- **The library** (pratiche-napoli): `layout.signatures`, `forms[].signing`, `steps[].gate`, and
  the validator rules, as above; `status` reuses the library's `verified|unverified|disputed`.
  The TARI Mod. 1 has its signature and date boxes measured on the pinned form, and every
  `signing` entry there is `unverified`.
- **`fill.py --sign`** files the request after the checks and before the pratica record (an open `pronta` pratica is resumed: only the request is filed again; `--channel` picks the channel), names the
  command the user runs (with `--vault <name>` where needed), and records it in `report.json`
  and `prossimo_passo`. The pilot's `regress.py sign` plays the user's review over the
  socket on the real Mod. 1 and checks the signature and date land in their boxes only.

## Decisions, as taken

All were taken as recommended; D8 is refined in "As built" (the user chooses when a way is unverified).

**D1. How to stamp the PDF.** (a) A pure-Go library inside the vault, with no CGO
(pdfcpu stamps images; to be confirmed in the spike), (b) a Python helper launched by the
vault, which brings a runtime dependency into a Go-only binary. *Recommend (a); if the
spike fails, say so and reconsider rather than move the image out of the vault.*

**D2. Where the user reviews.** (a) The CLI writes the candidate to a private file and opens
the default viewer, (b) the onboarding page shows it in a frame, which needs a CSP change
to the hash-pinned page. *Recommend (a): a real PDF viewer, no CSP change.*

**D3. A typed confirmation after viewing.** Recommend yes (the 8 hash characters), because
it makes approval follow looking.

**D4. Touch ID prompt source.** The helper reads name and hash from `summary` itself
(recommended) versus taking them as arguments, which a caller could set to something
harmless.

**D5. Where `signing` lives.** Per form and channel (recommended: one form takes different
signatures by channel) versus per channel only.

**D6. Date and place.** The vault writes the date; a place (`Napoli, lì`) is not written
until a form's layout asks for it, then from `residenza.comune`. *Recommend date only.*

**D7. Keeping the signed PDF.** Recommend not storing it in the vault (the user's pratica
folder holds it; one more copy of personal data, under no retention rule). Revisit with
the retention work.

**D8. The fallback when the library is silent.** When the library has no `signing` for
a channel, the skill hands the step over exactly as today. Recommend that default, never
a guess from other comuni.
