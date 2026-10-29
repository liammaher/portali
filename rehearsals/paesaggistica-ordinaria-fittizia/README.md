# Autorizzazione paesaggistica ordinaria: generated from the batch crawl, run to the declarations

On Napoli's SUE (art. 146 D.Lgs. 42/2004), for the CILA rehearsal's architect (with procura) and
client: a wooden pergola on the roof terrace and new outdoor paving, on a property under art. 136
and 142. Made-up data; the draft is "PROVA NON INVIARE" and stops before the declarations, the
attachments (the relazione paesaggistica, DPCM 12/12/2005), the bollo, signing and sending.

- **Crawl**: `catalogue/F839/sections/sue-autorizzazione-paesaggistica-ordinaria.json`; one section
  of its own, the rest shared with the semplificata.
- **Its own section** (*dichiarazione relativa ai contenuti dell'intervento*, a dictionary entry
  after the voltura's, which shares its name): the client's title and the deed that proves it
  (rogito, Agenzia delle Entrate or Conservatoria), building or area, use and PRG zone, deeds
  weighing on it, and the vincolo (yes, then art. 136 and/or 142). The vincolo's "sì" goes by the
  question beside it: "sì" alone also names the "no" above it.
- `supports` refuses art. 134 alone (the form names only 136 and 142), a tenant (the form offers
  no such title) and the marche da bollo route.

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-29 | pane | Stopped twice in its own section: the vincolo's "sì" by label found the "no" above it; then "in zona di PRG" named the "ad uso" box too (the page's nearest label before each box in a row is the last column's). Fixed in the dictionary and in inpage.js (a box whose own text is the label comes first). |
| 2026-10-29 | pane | **Filled**, resumed on the same draft: all 11 sections read back on a fresh load (8 s). Run as owner, rogito, building, no deeds, art. 136 and 142. |
