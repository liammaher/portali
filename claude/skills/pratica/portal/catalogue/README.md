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
- `<comune>/sections/<pratica>.json`: an empty draft of one pratica crawled by `crawl.js`:
  each section's fields (id, kind, label), each choice tried with what it opens, hides and
  shifts, and the attachments screen's slots (`slots`, read on the screen). `cila.json` and
  `fine-lavori.json` so far. `gen.py` writes a steps file from one of these, the dictionary
  (`../dictionary/fnsuap.yaml`) and a recipe (`rehearsals/*/recipe.yaml`).

## How it's made

In the Claude browser pane, logged in, on a scratch draft named "PROVA NON INVIARE -
catalogo <sportello>" (Liam deletes it afterwards):

- `wizard.js`: the wizard tree. Takes the defaults past the operations (the preselected
  destinatario, every Si/No answered No, no box ticked), steps back with "Pagina precedente",
  confirms only the portal's "Proseguire senza salvare?" box, and reopens the draft from
  `/fnsuap/cmd/home` when the portal shows its error page. About 8 s per operation, 15 s
  with the scelte tried.
- `crawl.js`: the sections. Tries each radio, checkbox and short dropdown and undoes it with
  the section's Annulla. Never presses Salva.

Neither fills a value, saves a section, ticks a declaration, pays or sends.

## Limits

- One comune's view on one day: the portal changes, so a run checks each section's
  fingerprint first and stops on a difference.
- Operations are crawled one at a time; combinations (two operations in one pratica, an
  intervento eventuale) are only what each adds on its own.
- Names aren't identity: two sections with different titles can hold the same fields, which
  the section crawl settles.
