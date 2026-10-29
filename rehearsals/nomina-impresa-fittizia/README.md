# Comunicazione di nomina dell'impresa esecutrice: generated from the batch crawl, run to the declarations

On Napoli's SUE, for the CILA rehearsal's architect (with procura) and client, about an invented
permesso di costruire (999/2026) on the client's flat. Made-up data; the draft is "PROVA NON
INVIARE" and stops before the declarations, the attachments, any bollo, signing and sending.

- **Crawl**: `catalogue/F839/sections/sue-comunicazione-al-comune-di-nomina-impresa-esecutrice.json`. Every section was already in the
  dictionary; this recipe is the first to generate and run it.
- The firm, where there is one, is the P6 fixture's Edil Fittizia (`f1:`).

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-29 | pane | **Filled**: every section in 49 s, every value read back on a fresh load of the draft. |
| 2026-10-29 | pane | Clean reruns of all 23 SUE recipes from run.py's install.js and run.js, nothing touched between runs: first stopped on the fresh load ("PERSONA (Privato): 0 fields with this label": the section wasn't open yet; the same run unchanged then filled); second, with the runner waiting for a section to open, **filled** in 45.8 s, read back on a fresh load. |
