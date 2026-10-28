# Portal catalogue

What impresainungiorno offers a comune, crawled by program instead of walked by hand, so a
pratica nobody has mapped can still be filled. One folder per comune, named by its codice
catastale (`F839` is Napoli).

## Files

- `<comune>/sue-wizard.json`, `<comune>/suap-wizard.json`: the wizard of each sportello. Every
  operation the comune offers, and for each one alone: the screens after it, the pratica the
  portal generates (its adempimenti, the office each goes to, its sections in order), and
  each option of "Scelte eventuali" tried alone (a Si/No answered Si, a box ticked) with the
  sections it adds or drops. Sections are indices into `sections`.
- `<comune>/sections/<pratica>.json`: one pratica's sections, crawled by `crawl.js`: each
  section's fields (id, kind, label), each choice tried with what it opens, hides and shifts,
  its fingerprint, and the attachments screen's slots (`slots`, read on the screen). A batch
  crawl (`sue-*`, `suap-NNN-*` for sector NNN of `suap-wizard.json`) also holds the pratica the
  portal generated (`generated`: adempimenti and sections), the wizard's screens and each
  scelta's sections (`scelta`); a section seen before is only a reference (`same_as:
  "<pratica>#<section>"`), which `gen.py` follows. The first ones, by hand: `cila`, `cil`,
  `scia`, `inizio-lavori`, `fine-lavori`, `agibilita`, `acconciatore`.
- `<comune>/drift.json`: what the last drift check found changed, per pratica (below).

## How it's made

In the Claude browser pane, logged in, on a scratch draft named "PROVA NON INVIARE -
catalogo <sportello>" (Liam deletes it afterwards):

- `wizard.js`: the wizard tree. Takes the defaults past the operations (the preselected
  destinatario, every Si/No answered No, no box ticked), steps back with "Pagina precedente",
  confirms only the portal's "Proseguire senza salvare?" box, and reopens the draft from
  `/fnsuap/cmd/home` when the portal shows its error page. About 8 s per operation, 15 s
  with the scelte tried.
- `crawl.js`: the sections. Tries each radio, checkbox and short dropdown and undoes it with
  the section's Annulla. Never presses Salva. `__crawl.batch({draft, ops, variants})` walks
  many pratiche on the one scratch draft with `wizard.js`'s moves (sector, operation, forward
  with the defaults, each scelta alone): a section whose fingerprint it has seen is not
  crawled again. Each pratica goes to the pane's `localStorage` as it's done (`crawl:op:*`),
  so a batch stopped by a logout resumes where it stopped. 20 to 60 s a pratica once the
  common sections are known (the first restaurant pratica: 140 s).
- `catalogue.py`: `ops sue|suap` lists a batch's operations, `known` the fingerprints the
  stored crawls have, `import` writes an exported batch here (refusing one that holds the
  login's name: `CATALOGUE_PRIVATE`), `count` the distinct sections, how many pratiche use each
  and whether the dictionary fills it (gen.py's own test, not a name match).

Neither fills a value, saves a section, ticks a declaration, pays or sends.

## Drift

A section's fingerprint is a hash of what it shows before any choice: ids, kinds, labels,
lengths, short dropdown lists. `__crawl.batch({..., check: true})` reads only those (about 1 s
a section) into `crawl:check:*`; `catalogue.py drift EXPORT.json` compares them with the
stored crawls and writes `drift.json`. A pratica with a difference is refused by `gen.py` and
by `run.py preflight` until it is crawled again (`import` clears it).

## Limits

- One comune's view on one day: the portal changes, so a drift check (above) stops runs of a
  changed pratica. Between checks, the runner's label checks still stop a run on a moved box.
- The fingerprint is the section before any choice: a change only inside a branch is caught by
  the runner, not by the check.
- Operations are crawled one at a time; combinations (two operations in one pratica, an
  intervento eventuale) are only what each adds on its own.
- Names aren't identity: two sections with different titles can hold the same fields, which
  the section crawl settles.
