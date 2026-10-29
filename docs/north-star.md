# North star

Status: **decided, 2026-10-27.** The direction below comes from the strategy work in [`reports/`](../reports/),
and Liam took all eight decisions at the end, each as recommended. The design rules in
[`CLAUDE.md`](../CLAUDE.md) and the design docs (local-first, credentials never over TCP, the vault
never goes online, no SMTP for PEC) describe the code as it is: each one changes in the pull request
that implements the decision affecting it, not before.

## The north star

**Any Naples business's own Claude can complete any dealing with a Naples public office, correctly,
and the owner only taps to approve.**

"Complete" is the point. Today people ask Claude about a pratica, get a general answer, and then do it
themselves, pay a commercialista or an agency, or put it off. Claude doesn't fill Naples forms or work
Naples portals on its own. The product exists to change that: Claude does the pratica, the owner
approves.

Take "do my TARI":

| | Plain Claude | With the vault and the skills |
|---|---|---|
| **Who and where** | Doesn't know; asks for name, codice fiscale, address | Reads them from the vault, already validated, with the municipalità worked out |
| **What applies** | Searches the web, meets stale and contradictory pages, asks more questions | The library says which TARI procedure, form version, deadline and channel |
| **How to do it** | Doesn't know the portal or the form; works it out slowly, or gives up and explains | The skill knows the steps, the boxes, the documents, the PEC address |
| **Doing it** | Hands it back to the owner | Fills it, prepares the PEC, and stops only for the owner's taps (SPID login, signature, send) |
| **Time** | Long, and often unfinished | Minutes |

The same holds for every administrative task, for every segment: "renew my dehors" (the vault knows
the locale, its m² and street; the skill knows the SUAP route and the canone), "file the CILA for via
Toledo 12" (the client, the procura and the catasto data; the SUE route on impresainungiorno), "renew
this client's permesso" (the client's documents and expiry; the Naples checklist, the postal kit, the
Questura's real timing).

It stops short in three places:
- **What specialists already do** (taxes, payroll, invoices): Claude coordinates them through their
  connectors; we don't redo their work.
- **What the law keeps for the person:** SPID logins, signatures, payments. Claude prepares everything
  up to those taps.
- **What only happens in person:** Questura appointments and fingerprints, desk-only offices, the
  building archive. Claude books, prepares and says exactly what to bring, but someone still has to go.

"Naples" means the Comune di Napoli and the local offices that serve it (Comune, Municipalità, ASL Napoli
1, Questura, Prefettura, Soprintendenza, Genio Civile, Camera di Commercio, SUAP and SUE on
impresainungiorno), plus the national bodies a Naples business deals with.

## The product

**One connector added to the owner's own Claude** (working name: *Napoli for Claude*). Add it once,
answer about ten questions in one chat, approve each connection once, and the assistant:

- completes the pratica: picks the right procedure, form and channel, fills in his facts, attaches the
  documents, works the portal, and hands him only the steps the law keeps for him;
- knows Naples: what applies, what's due and when, what a notice means, what to do;
- knows the business: the facts every form needs, validated;
- can act where Claude can't: PEC and signing;
- gets smarter from every other business's experience.

The owner never opens a PEC, reads a letter or chases anyone. They answer questions and tap: sign, pay,
send. Those taps are what the law requires (SPID, firma digitale, moving money), not what we choose.

The connector doesn't watch inboxes or wake itself up: the assistant does that (Claude Duet, once it
reaches Europe). It doesn't do accounting, payroll or payments: the specialists do (Fatture in Cloud,
JetHR, Stripe, the bank, the commercialista). It doesn't hold secrets the agent mustn't read: 1Password
does (SPID login, CIE PIN and PUK, PEC and portal passwords, cards).

## The five jobs

| The assistant needs to… | Job | What we have | What's missing |
|---|---|---|---|
| **complete the pratica** | Execution skills, one per procedure | The pratica skill (Claude Code only); the library's steps with the human-only ones marked, forms mapped to facts, what you do on each portal; `fill.py` with layouts for 4 of the library's 31 forms | Skills that work in the Claude app, one per procedure: which choices to make on each portal, what goes in each box, which documents to attach, where to hand over (SPID login, signature, payment); portal maps, screen by screen (open question 1); steps that name the owner's taps (open question 3); layouts where a filled PDF is sent (open question 4); **a real pratica completed end to end** (none yet) |
| **know the rules** | Naples layer | [pratiche-napoli](https://github.com/liammaher/pratiche-napoli): 30 sourced procedures (L1), 7 situations, offices by area, forms mapped to facts, signing rules by channel | A connector serving it; "what applies" as rules a program can check; recurring dates; notice recognition with deadlines from delivery; L2/L3 confirmation; building-works and immigration procedures |
| **know the business** | Profile | This repo's schema, Italian validation, the street → municipalità/quartiere/ASL lookup, people and roles, documents, dates, pratiche | Hosting it per business, encrypted; filling it from a visura and photos in one chat; the tier changes in [`reports/Vault sensitivity ranking.md`](../reports/Vault%20sensitivity%20ranking.md) |
| **act where Claude can't** | PEC, signing | The read-only PEC tools; the sign gate, which stamps a handwritten signature (enough for PEC, email, post and desk) | Both run only on a Mac today; sending PEC on the owner's tap; renewing Aruba's 90-day app password; a firma digitale for portal filings (open question 2); approval taps that work once hosted (open question 5) |
| **know what really happens** | Outcomes | The library's reports backend, with its privacy rules | Deployment; a tool for the assistant to report an outcome; outcomes collected before launch; a stricter tier for immigration |

Full list, item by item: [`reports/MVP checklist.md`](../reports/MVP%20checklist.md).

## Who it serves

One engine, filled segment by segment:

- **Owners** (a pizzeria, a shop): they mostly benefit from what others report.
- **Intermediaries** (architects, geometri, commercialisti): dozens of pratiche a year, so they
  contribute most of the outcomes and can set owners up.
- **Migrant-services organisations** (like an impresa sociale doing permesso paperwork): the heaviest
  use of the profile (many clients' data, the strictest tier) and the most sensitive outcomes.

Start where the segments meet: opening and running a business (ComUnica, SUAP, CILA/SUE, dehors,
insegna, TARI non domestica). See [`reports/Three avatars synthesis.md`](../reports/Three%20avatars%20synthesis.md).

What it's worth to each, honestly: an owner meets these pratiche a few times a year, but each one goes
from "pay someone or lose days" to "tap to approve". Intermediaries and migrant-services organisations
meet them every week, so for them it's large. It doesn't make offices answer faster, and it doesn't
lower what's owed.

**The tension with intermediaries.** If owners can file simple pratiche through Claude, commercialisti
and agencies lose that work. The pitch to them is "do more pratiche, faster, with fewer requests for more
documents", never "your clients won't need you".

## First milestone: one real pratica, end to end

Nothing has completed a real pratica yet. The closest is the TARI Mod. 1 draft filled from the vault,
which matched a hand-filled form box for box (11 of 11). Before building wide, take one or two pratiche
from the business-opening chain all the way through with the skills, on the real portal, up to the
owner's taps, and record where it breaks. That's also the first L3 in the library. Which pratica, and
why an intermediary's is the easier start: open question 6.

## How we'll know it works

- Pratiche completed end to end, with only the owner's taps.
- Requests for more documents, and rejections, compared with doing it by hand.
- Instalments and PEC deadlines not missed.
- Days from "I need to do this" to filed.
- Intermediaries reporting outcomes each month.

## Open questions

Found on 2026-10-27 by checking this page against both repositories. None is decided; each says what
we'd suggest.

### 1. Portals need a browser, and the phone has none

24 of the library's 30 procedures file on a portal first (18 only on a portal), mostly impresainungiorno
behind SPID; the other 6 are the 5 TARI ones, by PEC, and one at a desk. A connector alone can't work a
portal: Claude needs a browser, which is the Claude desktop app's own (Cowork's built-in browser, the
Code tab's pane), never Claude in Chrome (`design/packaging.md`). So "the owner only
taps" works from a phone for PEC pratiche once the connector is hosted, not yet for portal ones.

**Suggested:** say so: portal pratiche from a desktop, or through an intermediary, until a browser agent
reaches the phone or the cloud. Each execution skill carries a portal map, screen by screen: what to
choose, what goes in each box, what to upload.

### 2. Portal filings need a firma digitale, which the sign gate doesn't make

The sign gate stamps a handwritten signature, which PEC, email, post and desk accept (the library's
`contract/signing.json`). SUAP and SUE uploads need a firma digitale, and `signing.json` has no rule for
portals.

**Suggested:** a firma digitale handoff: the connector prepares the exact PDF, the owner signs it in their
remote-signature app (Aruba, InfoCert, Namirial) with one tap, and the connector checks that the signed
file is the one prepared. Or an intermediary signs under a procura speciale, as most SUAP filings are
done today. Add the portal rule to `signing.json`.

### 3. `human_only` is broader than the owner's taps

178 of the library's 349 steps are `human_only`, whole portal steps included ("log in, fill in the
istanza, upload, send"). Here Claude does those, and only the taps are the person's.

**Suggested:** replace the flag in the library's contract with the tap a step needs (`login`, `sign`,
`pay`, `send`, `in_person`), so an execution skill knows exactly where to stop.

### 4. More PDF layouts, or not

The five jobs list layouts for the flat PDF forms as missing; [`reports/Gennaro strategy
conclusion.md`](../reports/Gennaro%20strategy%20conclusion.md) says to stop making more.

**Suggested:** layouts only for the business-opening chain's forms that a portal or a PEC takes as a
filled PDF. Fill them in the connector, so the Claude app gets the filled PDF from a tool call.

### 5. Keys and taps once hosted

Decision 1 moves the profile online. The vault's model doesn't carry over: the key only in memory after
the owner's password, credentials never over TCP, Touch ID or Windows Hello for approvals. A hosted
connector holds plaintext while it serves Claude.

**Suggested:** design it before the second hosting step: per-business keys wrapped by a key service in
the EU, and an approval service of our own (a passkey tap on our page, reached from the connector) for
the sign gate, sending PEC and personal-tier reads. Check whether the Claude app supports MCP's URL
elicitation for that hop.

### 6. Which pratica first

An intermediary already has SPID, a firma digitale, a desktop and the clients' procure, so questions 1
and 2 are already answered for them.

**Suggested:** a CILA filed by an architect partner on impresainungiorno: the library has it current for
the July 2026 SUE switch, and `fill.py` has its Allegato A layout. Alongside it, if a business is ready,
a TARI non domestica by PEC, to test that path.

### 7. Large attachments in the Claude browser pane

`inpage.js` attaches a file by building it in the page and handing it to the file box (`kind: file` in a
steps file). In a Playwright window (`run.py run`) any size works, because run.js never passes through
the conversation. In the pane run.js is one tool call's text, so `run.py page` carries at most 256 KB of
files: a receipt or a small scan, not a planimetria (often 1–5 MB). Tested on a local bench on
2026-10-28, the pane offers no other way in: its form tool can't set a file box, a public portal page
can't fetch or frame `localhost` (`ERR_BLOCKED_BY_CLIENT`, the app's own block), and `window.open`
replaces the tab, so a helper page can't hand a file over. A normal `https` fetch with CORS works.

**Suggested:** the connector puts each large file at a short-lived `https` URL that only the run can
read (one-time token, CORS limited to the portal's origin, deleted after the upload), and run.js carries
the URL instead of the bytes. That is the first time a document leaves the computer, so it waits for the
hosting design (decision 1, question 5): encrypted per business, in the EU, never kept. Until then,
large files are attached in a Playwright window or by the person.

**Built and checked (2026-10-29):** the runner side. `run.py page --file-urls` puts a file's URL, size
and sha256 in run.js; `inpage.js` fetches it in the portal's page and stops if the bytes differ (bench:
a 3 MB file attached byte-exact, a wrong file refused). On the real portal in the pane: a fetch from
the portal's page to another `https` origin with CORS works (no CSP blocks it), and a 5 MB PDF built in
the page uploads in 3 s and is still there, 4.95 MB, after Salva and a fresh reopen of the draft. End to
end: the owner CILA filled with a 5 MB FAC-SIMILE drawing served from a public test gist (raw link
pinned to its commit; gist's "latest" link answered 404 for minutes after the push), run.js 6.9 KB
(`rehearsals/cila-fittizia`). Left: the real host, the hosting design above.

## Parked

Good for a personal vault, not part of the product. Kept working, not developed further:

- household and family features;
- the Touch ID helper;
- the file index's cloud sources (OneDrive, Google Drive, Dropbox);
- the CLI as the interface;
- local backups;
- per-user playbooks in the pratica skill (the per-procedure execution skills are the first job, not
  parked).

Client features (retention, many clients, per-person export) wait for the migrant-services segment.

## Decisions

All eight were taken on 2026-10-27, each as recommended. Each section keeps the question it answered.

### 1. Hosted or local

The connector has to be remote for the Claude app on a phone. The vault's rules say it never goes
online and never takes credentials over TCP.

**Decided:** host it in the EU. Do it in two steps: first the parts with no personal data
(rules, notices, outcomes), then the profile and PEC, encrypted per business. Keep a local option for
organisations that must hold client data themselves. This makes us a GDPR processor: it needs a
data-processing agreement and a security review before the second step.

**Changes when implemented:** the README's "local-first, no cloud", `docs/design/file-index.md` D10, and the
credential and socket rules in `CLAUDE.md`.

### 2. Sending PEC

Claude can't send PEC, and the owner must never forward anything by hand. The PEC tools have no SMTP by
design.

**Decided:** add sending, only after an explicit tap on the exact message, checked the way the
sign gate checks a file (the message approved is the message sent), and audited.

**Changes when implemented:** the PEC rule in `CLAUDE.md` and `mcp/README.md`.

### 3. Where the connector lives

The plan put a read-only MCP in the library repo. The profile, PEC and sign gate live in this repo.

**Decided:** one connector service in this repo that reads the library's published JSON. The
library stays pure data, site and reports backend, and never depends on the vault.

### 4. Who comes first

The plan says owners; the analysis says intermediaries contribute the outcomes.

**Decided:** design the experience for owners, but recruit intermediaries first, as contributors
and as the people who set owners up. Start with the business-opening chain. The pitch to them is in
"Who it serves": more pratiche, faster.

### 5. Immigration

The plan puts residents' procedures out of scope until its Phase 3; the migrant-services analysis shows
the biggest unmet need and the vault's best fit.

**Decided:** second segment, after business opening, built with a partner (Sportellino, a
non-profit chatbot for migrants), with its own stricter privacy tier for outcomes.

### 6. An interim nudge before Duet

Until Claude can wake itself up in Europe, nothing starts the assistant on its own.

**Decided:** build nothing now. If the connector launches before Duet reaches Europe, add the
smallest notification that sends the owner into Claude, and remove it when Duet arrives.

### 7. The sensitivity tier changes

Seven changes are recommended in [`reports/Vault sensitivity ranking.md`](../reports/Vault%20sensitivity%20ranking.md)
(SPID provider and dates to standard, citizenship and permit type to sensitive, payment cards and
credentials to 1Password, drop the US-only fields).

**Decided:** make them in one schema change of their own, with the library's copy synced as usual.

### 8. Name and funding

**Decided:** pick the name with the first partner. Fund the hosting (a few euros per business per
month, an estimate) from Camera di Commercio programmes, Regione Campania calls, Fondazione CON IL SUD,
or AI-provider credits, as the plan already lists.

## Where the rest lives

- [`reports/`](../reports/): the strategy reports this comes from (three personas, the synthesis, the
  MVP checklist, the sensitivity ranking, the research on recurring obligations).
- [pratiche-napoli](https://github.com/liammaher/pratiche-napoli): the library.
- [`design/supported-flows.md`](design/supported-flows.md): what a supported pratica promises (one
  message of questions, a login, a finished draft) and the plan to get there: pre-flight, branch
  coverage, self-recovery.
- The earlier plan (25 Oct) is Liam's local plan file; it points here for what has changed.
