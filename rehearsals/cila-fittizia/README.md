# Test: is a pre-mapped CILA faster and more accurate than an architect?

A pre-registered test, written on 2026-10-28 before any mapping. Everything below (the
question, the personas, the clock, the pass bar) is fixed now so the result can't be bent
afterwards; a change to this file after the first run is recorded in "Changes" with its
reason. The product's promise is in `docs/design/supported-flows.md`: one message of
questions, a login, a finished draft.

## The question

For a supported CILA, from the architect's sentence to the review page, is our system
**faster than an architect doing it by hand**, with **zero differences from an independent
answer key**, and with no message to the architect other than the allowed ones?

The SCIA rehearsal (`rehearsals/pizzeria-fittizia/`) already showed that a mapped run fills
fast and exactly as intended. This test asks what that one didn't: whether what we intend is
right (the key), whether it's faster for the person (the clock and the baseline), and whether
it holds across cases and over time (the personas and the repeat).

## The pratica

The CILA for internal, non-structural works, filed on impresainungiorno's Sportello Unico
Edilizia of Napoli (library: `napoli/comune/cila-lavori-edilizi`, L1, current for the 6 July
2026 switch). The online modules follow the Mod.CILA-23-06-2025: 87 fields, of which 21 come
from the vault, 2 are built from vault fields, 19 stay blank by design and 42 are answers,
most of them the architect's professional choices. Alongside: the Allegato A Programma 100,
which `fill.py` fills from its layout, and up to 14 attachments.

**Who the user is.** The architect, not the property owner: she files with a procura
speciale from her client. Her studio is a business vault (`~/.pvault-studio-fittizio`): she
is its titolare (with `albo.*`), each client is a person with `ruolo.tipo: cliente` and a
`procura`. The vault has no category for a client's property (address, catasto, destinazione
d'uso), so those come in as answers; that cost is measured, not hidden (see "Metrics").

## The stop point ("the review step")

The run ends when all of this holds:

- the draft is saved on the SUE with every factual section saved and read back;
- the Allegato A is filled and checked by `fill.py`;
- every attachment that needs no signature is uploaded (in the Playwright window; in the
  pane, files over 256 KB are listed as the architect's step, see "Two browsers");
- the fresh load of the draft found every value still there;
- the review page is open, listing what's left: sign the procura, the Allegato A and the
  Modello DP digitally and attach the `.p7m` files, pay the 151 € diritti and attach the
  receipt, then sign and send the CILA.

Nothing after that point is part of the test, and none of it is ever done here.

## Personas

Fictitious people and properties. Every codice fiscale and partita IVA passes its checksum,
and we choose them so they're unlikely to belong to anyone; no real person's data. All five
are clients of the same studio: architetto Chiara Fittizia, Studio Fittizio, Napoli.

| | Persona | What it exercises | Expected |
|---|---|---|---|
| P1 | Maria Fittizia, sole owner of a flat in Fuorigrotta (not zona A, no vincolo); moves a partition and redoes the bathroom; starts on a given date; one impresa esecutrice; D.Lgs. 81 applies, no notifica preliminare | The core path | Draft at the stop point |
| P2 | Bar Fittizio s.r.l., works in its own premises in the Arenella (the company is the titolare, its legale rappresentante signs) | A company as titolare, `business.*` data, "in qualità di" | Draft at the stop point |
| P3 | Luca Fittizio, a tenant, with the landlord's written assent | Non-exclusive title (a.2) and the assenso dei terzi attachment | Draft at the stop point |
| P4 | Anna and Paolo Fittizi, co-owners | Two titolari in "Soggetti coinvolti" | Draft at the stop point |
| P5 | A flat in a listed building in the centro storico (zona A, bene culturale) | A branch we don't support: Soprintendenza assent, the zona A attestation | **Refused at pre-flight** with a plain reason; the portal never opens |
| P6 | **The hard case** (`dossier-p6.md`): Luca Fittizio, a tenant, renovating a flat in the Arenella owned by Immobiliare Fittizia S.r.l.; works already started (a late CILA, d.2, with the reduced penalty); two firms on site, so the notifica preliminare | Every complication at once: non-exclusive title with the owner company's assent, works in progress, two impresa esecutrici, D.Lgs. 81 | Draft at the stop point, or refused at pre-flight if the first walk shows the late CILA can't be done online |

The fixtures (`fixture-p1.json` … `fixture-p5.json`, the studio's vault plus that client) and
the FAC-SIMILE attachments are written before the answer key and given unchanged to everyone.

## The answer key

- **Who writes it:** an architect or geometra who files CILAs in Napoli, from each persona's
  dossier and the official Mod.CILA only, never seeing our map or steps file. If no partner
  is available in time, Liam writes it from the dossier, the Mod.CILA and its notes, and the
  result says "weaker key" next to every accuracy figure.
- **What it holds** (`answer-key/p1.yaml` …): for each portal field and choice its expected
  value; the attachments expected; the steps left to the person.
- **When:** committed before the first mapping walk (the git history shows the order). The
  person mapping doesn't read it until the run is scored.
- **How a run is scored:** a person compares the saved draft with the key, field by field,
  on a fresh load of the draft (or on the CILA PDF the portal generates, if it shows one
  before signing), not on our review page, since our checks compare with our own plan.

## The human baseline

- **Who:** the same architect if possible, otherwise another professional who files CILAs.
  A baseline by Liam (not a professional) is recorded but marked as weaker.
- **What they get:** P1's and P2's dossiers, the same FAC-SIMILE files, logged out, at the
  portal's start page.
- **What's timed:** from opening the dossier to the same stop point, screen-recorded. Their
  draft is scored against the same key, and their mistakes counted.

## The clock

- **Start:** the architect's sentence in a fresh Claude session set up as a user's would be
  (the desktop app, the skills, the studio's vault), e.g. "file the CILA for Maria Fittizia,
  via … 12: she's moving a partition and redoing the bathroom".
- **Stop:** the review page is shown, with the stop point reached.
- **Timestamps recorded:** the sentence; the questions shown; the answers given; the login
  prompt; login done; the runner starting; the draft saved; the fresh load done; the review
  shown.
- **Two times:** wall time (sentence to review page) and the architect's hands-on time
  (answering, Touch ID, logging in).

## Allowed messages

Only these may reach the architect; anything else is off-script and counted:

1. one message of questions from pre-flight (a second only if an answer opens a branch);
2. the Touch ID prompt and the login prompt;
3. the review page with what's left.

## Two browsers

The primary run is in the Claude browser pane, the product's browser. Attachments over
256 KB are then the architect's step there (`docs/north-star.md`, open question 7), which is
a known limit, not a failure. A second run of P1 in the Playwright window uploads them, to
measure the full stop point.

## Pass bar

Per supported persona (P1–P4, and P6 if supported):

1. **Accuracy:** zero differences from the answer key, fields, choices and attachments.
2. **Speed:** wall time at most half the human baseline's; hands-on time at most a quarter
   of it; login to saved draft under 3 minutes in the window, under 4 in the pane.
3. **No off-script messages.**

And overall:

4. **P5** is refused at pre-flight, the portal never opened.
5. **Repeat:** P1 again seven days later, with no change to the steps file, passes 1–3.

Any persona failing 1 fails the test. A run is repeated, not failed, when the portal or SPID
is down. When a registry rejects fake data (catasto, albo number, P.IVA), that's the limit
of fake data: the fixture is fixed once per persona, the reason recorded here, and the run
repeated; a second rejection on the same persona counts as a fail.

## Metrics, beyond pass or fail

- Questions asked per persona, and how many a client-property category in the vault would
  remove (`ask.*` items that are property facts).
- Model calls between login and saved draft (expected: none, unless a recovery ran).
- Recoveries: how many, on which screen, and the time they took.
- Our own checks against the key: any field our checks passed but the key marks wrong is a
  mapping error and is listed separately, the most important finding this test can make.

## Before the first run

From `docs/design/supported-flows.md`: pre-flight and branch coverage built, so the
questions come in one message and P5 can be refused. Then the first walk maps the SUE
screens (`portal-map.yaml`, `portal-steps.yaml`, written screen by screen). Things the first
walk must settle and record here:

- **Who may log in.** The SUE service is for professionals. If the portal requires the
  logged-in SPID to be the progettista, Liam can't walk it alone and the test needs the
  partner architect at the keyboard for each login.
- **Validation of fake data:** codice fiscale, catasto foglio and particella, albo number.
- **The front office:** whether the SUE modules use the same widgets as the SUAP's.

## What the first walk settled (2026-10-28)

`portal-map.yaml` has every screen up to the attachments; `schema/schema-vs-portal.md` the
official schema against it.

- **Who may log in:** anyone with SPID. Liam (not an architect) walked it all. The procuratore's
  section opens with the logged-in name and CF but takes another person's (a made-up architect)
  without complaint. The test needs no partner at the keyboard to map; whether the SUE checks
  the signer at sending is past the stop point.
- **Fake data:** made-up codici fiscali, P.IVA and albo number save with no warning. A made-up
  catasto reference saves with "Attenzione! Dati catastali sconosciuti" on each field, and an
  address once showed "Indirizzo sconosciuto": orange warnings, values kept, draft not blocked.
  Whether they block sending isn't known (never sent). Every fake-data run will show them.
- **The late CILA:** online, in quadro d: "intervento in corso di esecuzione (art. 6-bis c. 5)",
  the start date, and an upload box in the quadro for the 333 € receipt (plain PDF accepted).
  The works-done case (d.3, 1000 €) is online too, against the paper Mod.CILA's "procedura
  cartacea". No pagoPA on the SUE ("Non sono presenti metodi di pagamento"): fees are paid
  outside the portal and the receipts uploaded.
- **Same widgets as the SUAP:** yes, the same front office (fnsuap, PrimeFaces): the Scheda
  anagrafica, Ubicazione, Dati catastali and Diritti have the SCIA's ids, and the runner's tree,
  dropdown, lookup and upload all worked. New: ids renumber when a choice adds rows (a's
  "altro", h's DURC boxes), and a required field is marked by `.error-message-text`, which the
  runner doesn't read yet.
- **The stop point is reachable and is:** Allega documenti, after Pagamento oneri. Required: the
  portal's own procura (download, sign, back as `.p7m` only), the identity documents of the
  titolare and of who gives the procura (plain PDF), the portal's own "Quadro riepilogativo
  CILA DSC" (download, back as PDF), the elaborati grafici. Everything else has no slot of its own
  but Allegati liberi.
- **Drafts:** reopened from "Pratiche in bozza" with every value kept. An old draft address
  (`execution=eXsY`) gives the portal's error page once the flow has moved on, so
  `run.py --resume` must reopen from the list, not from report.json's url.
- **Mapping by program:** `crawl.js` mapped i), the relazione tecnica and the diritti (and
  re-mapped b, d, a, h) in about a minute, trying each choice and undoing it with Annulla; every
  branch it found is in the official schema.

## Lines we don't cross

As in `rehearsals/pizzeria-fittizia/README.md`: never send, never pay or create a pagoPA
position, never tick a declaration or sign in anyone's name, drafts marked "PROVA NON
INVIARE" and deleted afterwards, logins are the person's own, FAC-SIMILE on every attachment.

## Runs

| Date | Persona | Browser | Wall time | Hands-on | Login → draft | Key diffs | Off-script | Result |
|---|---|---|---|---|---|---|---|---|
| 2026-10-28 | P6 | pane | not clocked | not clocked | 83.7 s (wizard 8.3, 18 sections 63, fresh load 12.3) | 1 (weakest key, seen before mapping) | not measured | Draft filled and verified; stop point not reached (see below) |
| 2026-10-28 | P6 | pane | not clocked | not clocked | 101.9 s to Allega documenti (wizard 9, 18 sections ~71, fresh load 15.4, payment 2.2, 6 attachments 6.4) | 1 (as above) | not measured | Stop point reached except what is the person's (see below) |
| 2026-10-28 | P6 | pane | not clocked | not clocked | ~129 s to Allega documenti in two legs (wizard 10, 5 sections ~26, stop, resume: 13 sections ~64, fresh load 15.9, payment 2.5, 6 attachments 8.8) | 1 (as above) | not measured | From the **generated** steps file: same fields, all kept (see below) |

### 2026-10-28, P6, first run from the steps file

Not a scored test run: the clock from the architect's sentence, the questions message and
the review page weren't part of it, and the steps file grew during the session (the walk,
then `crawl.js` for i, the relazione and the diritti, then label lookup and "+" for the two
firms). It answers the narrower question: does the runner fill the whole CILA on the real
SUE from the vault and the answers, with no model call? Yes: draft "PROVA NON INVIARE -
CILA P6 run 2", 18 sections and 152 fields, every one read back the same on a fresh load.

Against `answer-key/p6.yaml` (scored by Claude, who wrote the key and saw it before
mapping), on the sections filled:

- **Same:** 4–18 (the client), 31–40 (the architect), 41–45 and 48 (title a.2, b.1, c.1 by
  choosing no other procedure, d.2 with the date and the 333 € receipt), 51–60 and 62–63
  (property and catasto), 65, 67–70, 74–78 (relazione: manutenzione straordinaria, the
  description, no tolerances), 79–81 left blank, the soggetti coinvolti (titolare, firms).
- **Different (1):** 66. The key chose g.1, "altri tecnici indicati in Soggetti coinvolti",
  reasoning that Chiara is also direttore dei lavori; on the SUE the direttore dei lavori is
  ticked in G1 and G2 is only for *other* technicians, of whom P6 has none, so the run left
  G2 empty. The key is probably what's wrong, but it counts until Liam rules on it.
- **Portal fields the key didn't foresee:** the owner's table (a.2: Giulia Esempio for
  Immobiliare Fittizia), the procuratore's qualifica, the oggetto, the firms' legal
  representatives and INPS/INAIL codes, the diritti's payment mode. The fixture gained the
  data they need (Changes).
- **Not reached, so the stop point isn't:** the Allegato A (`fill.py`), the plain-PDF
  attachments on Allega documenti (owner's assent, IDs, drawings), and the portal's own
  procura and CILA DSC summary (download, then back: the person signs the procura).

### 2026-10-28, P6 run 3: to the stop point

Draft "PROVA NON INVIARE - CILA P6 run 3", one run, no stop: the form as above, then
Pagamento oneri (no method on the SUE) and Allega documenti, where the runner put six
FAC-SIMILE PDFs in their slots by title (the client's ID twice, the owner's assent, the
notifica preliminare, the drawings, the relazione in Allegati liberi with its required
description) and Salva kept them all. Left, as the stop point says, to the person:

- **The procura:** the portal builds it from the draft (`/fnsuap/template.do?id=A_PRC5x11`,
  41 KB PDF, iText); fetched in the page's memory, it's the client's to sign and the
  architect's to sign digitally, back as `.p7m` only.
- **The Quadro riepilogativo (CILA DSC):** not built from the draft but the national blank
  checklist of attachments (`template.do?id=D3664`, PDFCreator, no form fields); required.
  Ticking it needs a `fill.py` layout (not written yet); then it's signed.
- **The Allegato A:** `fill.py` filled it for Luca from the synthetic P6 vault, all 15 values
  read back in their boxes. Two findings: the library's rule that only a titolare or legale
  rappresentante declares in a business vault refuses a client declaring in their own name
  (Liam's to decide; the library's tests hold the rule on purpose), and without `fontTools`
  PyMuPDF can't subset the embedded Arial Unicode, so the one-page PDF is 15 MB, over the
  portal's 9.77 MB (install fontTools, or fall back to a built-in font: the second needs the
  "nothing written outside a box" check to recognise it).
- The fee receipt (`.p7m`), the Modello DP, the declarations, signing and sending.

### 2026-10-28, P6 run 4: from the generated steps file

`portal-steps.generated.yaml` is no longer written by hand: `claude/skills/pratica/portal/gen.py`
wrote it from `recipe.yaml` (the operation, the scelta, the person's steps, `supports`), the
crawl of an empty CILA draft (`catalogue/F839/sections/cila.json`, 282 s, every section, every
choice tried) and the shared field dictionary (`dictionary/fnsuap.yaml`). `gen.py --check
portal-steps.yaml` finds it the same as the hand walk, field for field: every id, including the
21 of a) where two choices (title "altro", non-exclusive title) shift each other's rows, which
the crawl tried apart and `gen.py` adds up by their offsets. It adds one case the walk didn't
cover: diritti not due (0 €), from the amount.

Draft "PROVA NON INVIARE - CILA P6 generato": all 158 values (152 fields, 6 attachments) filled,
saved and read back again on a fresh load, nothing typed by hand. One stop, in Localizzazione:
the street's suggestion list was still open over "scala" (a timing the earlier runs didn't
hit). inpage.js now waits for the list to close (or closes it) after picking a street; the run
resumed on the same draft from that section.

## Changes

- 2026-10-28, before any mapping: added P6, the hard case, to put the flow under the most
  strain one CILA can carry. It goes first: `fixture-p6.json` (loaded through every vault
  rule with `TestPraticaPilotVault`), `dossier-p6.md` (what the key, the baseline and the run
  all start from) and `needs.json` (the run's read scope).
- 2026-10-28, before any mapping: Liam asked Claude to write P6's answer key
  (`answer-key/p6.yaml`) by common sense. Its author also maps the portal, so it is the
  weakest key: committed before the first walk, reviewed by Liam, and every P6 accuracy
  figure marked "weakest key". Writing it added four assumptions to the dossier (site size,
  Luca's tax standing, no PEC, the penalty receipt).
- 2026-10-28, before any mapping: no partner architect yet, so Liam writes the answer key
  and does the human baseline, as "The answer key" and "The human baseline" allow. Every
  accuracy and speed figure is marked "weaker key" / "weaker baseline" until a professional
  redoes them.
- 2026-10-28, first walk: the mapping session read `answer-key/p6.yaml` before mapping, against
  "The answer key" above. P6's key was already the weakest; every P6 accuracy figure now also
  says "key seen before mapping".
- 2026-10-28, after the walk: the portal asks for the firms' legal representatives and
  INPS/INAIL codes, the owner company's phone and its representative's residence, which
  `fixture-p6.json` lacked; added (made up, valid checksums). The DURC's Cassa edile box is
  left unticked (the vault has no field for it).
- 2026-10-29: the vault has a category for a client's property now (`immobile`, schema 19), so
  P6's address and catasto moved from `answers-p6.json` to `fixture-p6.json` (`luca:immobile.*`)
  and the SUE dictionary reads `c:immobile.*`: about 12 fewer answers per SUE pratica.
- 2026-10-28, first walk, stopped in quadro h): the official schema of the CILA (InfoCamere's
  `mod_cila_v1.0.7.xsd`) holds 137 of the 152 portal fields walked (`schema/schema-vs-portal.md`).
  The next step tests building the portal map from the schema plus a crawler instead of walking
  each screen by hand.

## Clean reruns (2026-10-29)

In the pane, from run.py's install.js and run.js, nothing touched between runs: first filled in 107.5 s; second, with the runner waiting for a section to open, **filled** in 148.5 s, read back on a fresh load.

## Real clients (2026-10-29)

Until now the dictionary held P6's one path: a tenant, works under way. It now maps every
title (proprietario, comproprietario, usufruttuario, amministratore di condominio, or "altro"
with its name; alone, or with the other holder's assent from a person or a company), every
answer of b) (common parts), d.1/d.2/d.3 (works to start on a date or after the assents, under
way, done), e) (any destinazione), f) (free, or a contributo paid or to pay), the direttore
dei lavori as the architect or none, h) with one firm, two, or none (in proprio), every work
type of the relazione, and the construction tolerances (art. 34-bis/34-ter, the 1-bis
percentage by floor area; Napoli is in seismic zone 2). Pre-flight refuses another technician
as direttore, other technicians already appointed, and other filings or assents outside the
pratica. Two more answer files run it: `answers-proprietario.json` (owner, condominium, to
start on a date, contributo paid, manutenzione + restauro, two tolerances, one firm, no
direttore) and `answers-comodatario.json` (comodatario with a co-owner's assent, works done,
contributo to pay, in proprio, "altri interventi").

| Date | Case | Browser | Login → draft | Result |
|---|---|---|---|---|
| 2026-10-29 | proprietario | pane | 79.8 s to Allega documenti | Stopped there: the screen had no co-holders' ID slot, and two new ones (the contributo's prospetto and receipt). Mapped by answer (`on_page` slots), resumed on the draft: **filled** |
| 2026-10-29 | comodatario | pane | 41.4 s to d) | Stopped after uploading the 1,000 € receipt: read back by label, its cell now shows the file's name. Named by the row above instead, resumed: **filled** (fresh load 13.7 s) |
| 2026-10-29 | P6 | pane | 91.3 s | **Filled**, fresh load 13.6 s: the same 160 values as before |
| 2026-10-29 | proprietario, clean | pane | 110.7 s | **Filled** from scratch, 20 sections, fresh load 22.2 s |
| 2026-10-29 | comodatario, clean | pane | 85.6 s | **Filled** from scratch, 20 sections, fresh load 12.8 s |
| 2026-10-29 | proprietario, 5 MB drawing by link | pane | 86.5 s (attachments 10.7 s) | **Filled**: the elaborati grafici a 5,195,674-byte FAC-SIMILE PDF, fetched in the portal's page from a public test gist pinned to its commit (`run.py page --file-urls`), sha256 checked; run.js 6.9 KB. Still there, 4.95 MB, on a fresh reopen of the draft |

Not run on the portal: a destinazione other than residenziale (the fixture's flat is
residenziale; pre-flight and the generator cover it), a terzo titolare company other than P6's,
two tolerances other than 1-bis and c. 2.
