# North star

Status: **proposed, 2026-10-27.** The direction below comes from the strategy work in [`reports/`](../reports/).
The open decisions at the end aren't taken yet. Until they are, the design rules in
[`CLAUDE.md`](../CLAUDE.md) and the design docs (local-first, credentials never over TCP, the vault
never goes online, no SMTP for PEC) stay exactly as they are, even where this document points away
from them.

## The north star

**Any Naples business's own Claude can handle any dealing with a Naples public office correctly, and
the owner only taps to approve.**

"Naples" means the Comune di Napoli and the local offices that serve it (Comune, Municipalità, ASL Napoli
1, Questura, Prefettura, Soprintendenza, Genio Civile, Camera di Commercio, SUAP and SUE on
impresainungiorno), plus the national bodies a Naples business deals with.

## The product

**One connector added to the owner's own Claude** (working name: *Napoli for Claude*). Add it once,
answer about ten questions in one chat, approve each connection once, and the assistant:

- knows Naples: what applies, what's due and when, what a notice means, what to do;
- knows the business: the facts every form needs, validated;
- can act where Claude can't: PEC, signing, exact PDF forms;
- gets smarter from every other business's experience.

The owner never opens a PEC, reads a letter or chases anyone. They answer questions and tap: sign, pay,
send. Those taps are what the law requires (SPID, firma digitale, moving money), not what we choose.

The connector doesn't watch inboxes or wake itself up: the assistant does that (Claude Duet, once it
reaches Europe). It doesn't do accounting, payroll or payments: the specialists do (Fatture in Cloud,
JetHR, Stripe, the bank, the commercialista). It doesn't hold secrets the agent mustn't read: 1Password
does (SPID login, CIE PIN and PUK, PEC and portal passwords, cards).

## The four jobs

| The assistant needs to… | Job | What we have | What's missing |
|---|---|---|---|
| **know the rules** | Naples layer | [pratiche-napoli](https://github.com/liammaher/pratiche-napoli): 30 sourced procedures (L1), 7 situations, offices by area, forms mapped to facts, signing rules by channel | A connector serving it; "what applies" as rules a program can check; recurring dates; notice recognition with deadlines from delivery; L2/L3 confirmation; building-works and immigration procedures |
| **know the business** | Profile | This repo's schema, Italian validation, the street → municipalità/quartiere/ASL lookup, people and roles, documents, dates, pratiche | Hosting it per business, encrypted; filling it from a visura and photos in one chat; the tier changes in [`reports/Vault sensitivity ranking.md`](../reports/Vault%20sensitivity%20ranking.md) |
| **act where Claude can't** | PEC, signing, forms | The read-only PEC tools, the sign gate, `fill.py` with four layouts | All three run only on a Mac today; sending PEC on the owner's tap; renewing Aruba's 90-day app password |
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

## Parked

Good for a personal vault, not part of the product. Kept working, not developed further:

- household and family features;
- the Touch ID helper;
- the file index's cloud sources (OneDrive, Google Drive, Dropbox);
- the CLI as the interface;
- local backups;
- per-user playbooks in the pratica skill;
- more per-form PDF layouts beyond the existing ones.

Client features (retention, many clients, per-person export) wait for the migrant-services segment.

## Open decisions

Each has a recommendation; none is taken.

### 1. Hosted or local

The connector has to be remote for the Claude app on a phone. The vault's rules say it never goes
online and never takes credentials over TCP.

**Recommendation:** host it in the EU. Do it in two steps: first the parts with no personal data
(rules, notices, outcomes), then the profile and PEC, encrypted per business. Keep a local option for
organisations that must hold client data themselves. This makes us a GDPR processor: it needs a
data-processing agreement and a security review before the second step.

**Changes if taken:** the README's "local-first, no cloud", `docs/design/file-index.md` D10, and the
credential and socket rules in `CLAUDE.md`.

### 2. Sending PEC

Claude can't send PEC, and the owner must never forward anything by hand. The PEC tools have no SMTP by
design.

**Recommendation:** add sending, only after an explicit tap on the exact message, checked the way the
sign gate checks a file (the message approved is the message sent), and audited.

**Changes if taken:** the PEC rule in `CLAUDE.md` and `mcp/README.md`.

### 3. Where the connector lives

The plan put a read-only MCP in the library repo. The profile, PEC and sign gate live in this repo.

**Recommendation:** one connector service in this repo that reads the library's published JSON. The
library stays pure data, site and reports backend, and never depends on the vault.

### 4. Who comes first

The plan says owners; the analysis says intermediaries contribute the outcomes.

**Recommendation:** design the experience for owners, but recruit intermediaries first, as contributors
and as the people who set owners up. Start with the business-opening chain.

### 5. Immigration

The plan puts residents' procedures out of scope until its Phase 3; the migrant-services analysis shows
the biggest unmet need and the vault's best fit.

**Recommendation:** second segment, after business opening, built with a partner (Sportellino, a
non-profit chatbot for migrants), with its own stricter privacy tier for outcomes.

### 6. An interim nudge before Duet

Until Claude can wake itself up in Europe, nothing starts the assistant on its own.

**Recommendation:** build nothing now. If the connector launches before Duet reaches Europe, add the
smallest notification that sends the owner into Claude, and remove it when Duet arrives.

### 7. The sensitivity tier changes

Seven changes are recommended in [`reports/Vault sensitivity ranking.md`](../reports/Vault%20sensitivity%20ranking.md)
(SPID provider and dates to standard, citizenship and permit type to sensitive, payment cards and
credentials to 1Password, drop the US-only fields).

**Recommendation:** make them in one schema change of their own, with the library's copy synced as usual.

### 8. Name and funding

**Recommendation:** pick the name with the first partner. Fund the hosting (a few euros per business per
month, an estimate) from Camera di Commercio programmes, Regione Campania calls, Fondazione CON IL SUD,
or AI-provider credits, as the plan already lists.

## Where the rest lives

- [`reports/`](../reports/): the strategy reports this comes from (three personas, the synthesis, the
  MVP checklist, the sensitivity ranking, the research on recurring obligations).
- [pratiche-napoli](https://github.com/liammaher/pratiche-napoli): the library.
- The earlier plan (25 Oct) is Liam's local plan file; it points here for what has changed.
