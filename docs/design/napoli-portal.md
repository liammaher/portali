# Napoli's impresainungiorno, end to end

The goal: a person says what they want to do, gives the context and their data (or it's in
their vault), and the pratica is done fast on impresainungiorno (Napoli's SUE for building
works, SUAP for businesses), up to the steps the law keeps for them: logging in, their
declarations, paying, signing and sending. `supported-flows.md` is the promise for one mapped
pratica; this is what it takes for the whole platform, in five pieces.

Status (2026-10-28): pieces 1 and 5 built for the SUE's common pratiche and one SUAP pratica;
3 and 4 partly; 2 not started as data. Vault PR #87, library PR #62.

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

Where it stands: SUE, 79 of 150 sections in the dictionary, 74% of section uses across its
27 pratiche; 6 SUE pratiche generated and run (CILA, CIL, SCIA, inizio and fine lavori,
agibilità). SUAP, 1 of about 283 pratiche (acconciatore, apertura). Each covers its plain
case.

Next: make `crawl.js` reuse one scratch draft (as `wizard.js` does) and crawl every SUAP
pratica unattended, which counts its distinct sections; then the dictionary in order of how
many pratiche use a section; test `draft.py` on a SUAP pratica it wasn't tuned on.

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

Today only fragments: each recipe's `supports` (the cases it refuses), a few case questions,
the pratiche-napoli library's procedures. Next: these tables as data in the library, for the
common cases first, each rule with its source and date, re-checked on a schedule; the facts
the law branches on asked of the person; anything uncovered refused at pre-flight.

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
