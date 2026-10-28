# Proroga del termine di inizio lavori: generated from the batch crawl, never walked

On Napoli's SUE, for the CILA rehearsal's architect (with procura) and client, the client
holding an invented permesso di costruire (PG/2026/0888888 of 15/09/2026). Made-up data; the
draft is "PROVA NON INVIARE" and stops before the declarations, the bollo, signing and sending.

How its steps file was made (`portal-steps.generated.yaml`, by `gen.py`):

- **Crawl**: the SUE batch (`crawl.js` batch on the scratch draft "catalogo SUE"):
  `catalogue/F839/sections/sue-proroga-inizio-lavori.json`, 14 sections, 3 new.
- **Its own sections**, written by hand into the dictionary (no national form covers them): the
  days asked and why; the property, its permit (a permesso di costruire; the DIA branch is
  mapped but not run), catasto, holder and new start date; the imposta di bollo (pagoPA, two
  marche: one for the application, one for the provvedimento; the marche route is mapped but
  not run). "Dichiarazione relativa alla sede dell'attività" is the CILA's Localizzazione
  block: the page says it's where "the activity or intervention" is.
- `supports` refuses the DIA and the marche routes until each is run.

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | First run, no stop: 11 sections in 82 s, every value read back on a fresh load (12 s). The made-up address shows the portal's orange "Indirizzo sconosciuto", as in the CILA rehearsal: values kept, draft not blocked. |
