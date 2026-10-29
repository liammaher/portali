# Cambio d'intestazione of a SCIA: generated from the batch crawl, run to the declarations

On Napoli's SUE: an invented SCIA passes from Immobiliare Fittizia S.r.l. to the CILA rehearsal's
client, with no co-holder and the same direttore and firm. Made-up data; the draft is "PROVA NON
INVIARE" and stops before the acknowledgements in the new holder's name, signing and sending.

- **Crawl**: `catalogue/F839/sections/sue-cambio-intestazione-cia-scia-dia.json`; its sections are
  the voltura's dictionary entries (title and deed, the old holder, the new holder's domicilio,
  who directs and builds the works).
- Its prescrizioni acknowledgement reads "di essere pertanto a conoscenza…" (the voltura's "di aver
  preso visione"): both are the new holder's (`theirs`).
- A co-holder needs the scelta "ci sono cointestatari", as in the voltura: not in this recipe.

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-29 | pane | The portal's "errore inatteso" after the Scheda anagrafica (left a draft); a new draft stopped at the prescrizioni acknowledgement (its wording). |
| 2026-10-29 | pane | **Filled**, resumed on the same draft: all 17 sections read back on a fresh load (11 s). |
| 2026-10-29 | pane | Clean reruns of all 23 SUE recipes from run.py's install.js and run.js, nothing touched between runs: first filled in 61 s; second, with the runner waiting for a section to open, **filled** in 64.7 s, read back on a fresh load. |
