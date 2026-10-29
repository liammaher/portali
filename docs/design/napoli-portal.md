# Napoli's impresainungiorno, end to end

The goal: a person says what they want to do, gives the context and their data (or it's in
their vault), and the pratica is done fast on impresainungiorno (Napoli's SUE for building
works, SUAP for businesses), up to the steps the law keeps for them: logging in, their
declarations, paying, signing and sending. `supported-flows.md` is the promise for one mapped
pratica; this is what it takes for the whole platform, in five pieces.

Status (2026-10-28): piece 1 built for the whole SUE and under way for the SUAP (batch crawl,
fingerprints, drift check, interventi); 5 built; 3 and 4 partly; 2 started as data (the SUAP's common cases). Vault PRs #87,
#88, #89 and this branch; library PR #62.

## 1. The portal map: every box, and what goes in it

Pratiche are built from shared sections, so the work grows with the distinct sections, not
the pratiche. `claude/skills/pratica/portal/`:

- `wizard.js` crawls the wizard (`catalogue/F839/`): the SUE's 27 operations with their
  sections and scelte; the SUAP's tree (105 sectors, 283 operations), not yet each
  pratica's sections.
- `crawl.js` reads an empty draft of one pratica (`catalogue/F839/sections/`): each box, its
  label, what each choice opens. Automatic, 2 to 13 minutes.
- `dictionary/fnsuap.yaml` says what goes in each section's boxes (a vault field, or a
  pre-flight question), once for every pratica that has the section.
- `draft.py` drafts a new section's entry from InfoCamere's national forms (`schema/`), for a
  person to check.
- `gen.py` writes a pratica's steps file from its crawl, the dictionary and a short recipe,
  or nothing if a box is unaccounted for.

Where it stands (2026-10-28): `crawl.js` crawls in batches on one scratch draft, each section
once by fingerprint (`catalogue/README.md`). SUE: all 27 pratiche crawled, 127 distinct
sections, 88 in the dictionary (88% of uses), 20 pratiche complete; 7 generated and run
(CILA, CIL, SCIA, inizio and fine lavori, agibilità, proroga inizio lavori). SUAP: 278 pratiche
crawled so far (about half of the batch's 1,028 trips), 477 distinct sections, 67 in the
dictionary (71% of uses, counted under each title a section has), 107 pratiche filled end to end;
the openings' "interventi eventuali" (alcohol, an outside sign) crawled; companies and businesses
in the Registro delle imprese taken. Generated and run: the openings of a hairdresser, a
restaurant, a non-food shop, a bakery and rooms to let, the takeover and the suspension of a
shop and of a restaurant, a restaurant's new name, a shop's bigger sales area, a running
restaurant's alcohol notice and a restaurant's closure; written and waiting for the portal: an
opening with alcohol, one with an outside sign, a historic-centre authorization, a company's
suspension, a shop's new name and a restaurant's dehors. A drift check
reads every section's fingerprint (45 s a pratica) and stops runs of a changed pratica.

`draft.py` pairs boxes with the national schema by order and type, since the portal draws a
section in the schema's order (`schema/README.md`): on two SUAP pratiche it wasn't tuned on, 44
of 49 boxes right (33 before), and sections it marks `exact` held 17 of 18. Drafts are for a
person to check, never used unchecked. The pratica's XML, which would name each box's schema
field, exists only once a pratica is sent: the portal offers PDFs before (`rehearsals/cessazione-fittizia`).

Next: run the six waiting recipes; finish the SUAP batch and import it; the 75 pratiche one
or two sections short; a company's other partners (their art. 85
statements); the SUE's last seven pratiche with a professional (asseverazioni, landscape
classifications).

## 2. The legal layer: which pratica, which choices, which answers

From what the person wants to the pratica(s) to file, the wizard's scelte, and the legal
answers inside the forms (fire prevention, notifica preliminare, diritti, protections). The
hardest piece: the portal doesn't say which procedure the law requires, mistakes are costly
(a rejected pratica, a fine), the rules change quietly, and some depend on facts about the
property only maps or a professional can give.

Sources: D.Lgs. 222/2016, Tabella A (business activities to their regime: SCIA,
authorization, notice); the Glossario unico, DM 2 March 2018 (building works to edilizia
libera, CILA, SCIA, permesso di costruire); Napoli's and Campania's own rules (the 2026
historic-centre rule for food and drink openings, zones, the regolamento edilizio).

Started (2026-10-29, library branch `claude/legal-rules-table`): `rules/suap-napoli.yaml` in
pratiche-napoli. Eleven facts, each with its plain-Italian question (the activity, what happens
to it, the Piano's zone, the sales area, a sign, music, dehors, fire prevention, alcohol), and
41 rules from them to the procedure, its regime and the portal's sector and operation, for
shops, bars and restaurants, hairdressers and beauticians, and B&Bs. A rule cites a procedure's
source that backs a verified claim there, so the table states nothing of its own and the
source monitor watches it. Cases known and not handled are refused with a reason (a takeover in
zones A and B, over 250 mq, the Dogane notice for alcohol). The validator tries every
combination of the facts and fails on a gap or two rules that disagree; `decide()` gives the
pratiche, or the facts still to ask, or the refusals.

Next: pre-flight reads the table (the facts asked in its one message, the recipe picked from
the answer, a refusal stops the run), replacing each recipe's own `supports`; the Dogane
notice and the SUE's building works (the Glossario unico) as procedures, then as rules.

## 3. Intake: the person's context into answers

Everything the vault doesn't hold, asked in one message before the portal opens
(`run.py preflight`: `asks`, `groups`, proposals to confirm). Next: questions in plain words
(the drafted ones read like the portal's labels), and the legal layer's questions folded in.

## 4. Documents

What each pratica attaches. Built: attachments by slot title; a slot that takes only signed
files (`.p7m`) is left to the person; forms filled from the vault (`fill.py`, e.g. the CILA's
Allegato A). Next: the portal's own documents (the procura, the riepilogo checklists) filled
for signing. Drawings and technical reports stay the professional's.

## 5. Reliability

Built: every value read back after each save and again on a fresh load; a stopped run resumes
on its draft; each pratica run on the real portal with made-up people before it's offered;
offline tests (`bench/`). Next: a periodic re-crawl compared with the last one, so a changed
portal stops runs instead of misfilling them.

## How far

The common building pratiche: a few sessions (the legal layer and wording). The common
business pratiche: a few weeks. The whole platform: months, mostly the long tail of rare SUAP
pratiche, done as people need them.
