# SCIA edilizia with its autorizzazione paesaggistica ordinaria, one pratica

A SCIA condizionata on Napoli's SUE: the SCIA and the landscape authorization selected
together in the wizard (the SCIA's section b) says that's how an assent is asked with it).
The case: an owner's change of use of a whole building within its category, no works, with a pergola needing the ordinaria, the contributo paid during the works and worked out by the SUE. Made-up data (rehearsals/cila-fittizia's people and property); the draft is
"PROVA NON INVIARE" and stops before the declarations, payment, signing and sending.

- **Two operations** (`operation:` a list), their sections in the order of the combination's
  crawl (`catalogue/F839/sections/sue-combo-scia-paesaggistica-ordinaria.json`: fingerprints read on
  an empty draft on 2026-10-29). Every section is one already crawled: the SCIA's and the
  ordinaria's, each the same fingerprint as alone, so the dictionary fills it unchanged.
- **The asseverazione** names the ordinaria procedure ("ricade in zona tutelata e le opere comportano
  alterazione", then the procedure and its "Si allega"), and b) gets no start date: the works
  start once the SUE says the authorization is in.
- **Attachments**: the combination's screen was read on the empty draft (`slots`); the SCIA's
  answer-dependent slots are added from its own screen. the ordinaria's drawings, photos and earlier authorization take only signed files (.p7m); its relazione paesaggistica takes a plain PDF but is the progettista's, signed as for the ordinaria alone.
- `supports`: the SCIA's, plus the ordinaria's own refusals; `paesaggio` is fixed to ordinaria (another
  answer is another recipe).

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-29 | pane | First run: every section filled; stopped at Allega documenti on two required slots that take a plain PDF, the ordinaria's relazione paesaggistica and a second one the SCIA's "Si allega la relazione paesaggistica" adds. Both now take the architect's relazione (`relazione_paesaggistica`). |
| 2026-10-29 | pane | Rerun: the portal's "errore inatteso" page mid-run; rerun unchanged after 20 s. |
| 2026-10-29 | pane | **Filled** from scratch in 182 s, every value read back on a fresh load (31 s): an owner's change of use of a whole building, the contributo during the works worked out by the SUE. |
| 2026-10-29 | pane | As now, a single unit within its category with no works (a whole building's is a SCIA alternativa in Campania): first run stopped on the portal's error page at DOMICILIO ELETTRONICO, the draft not reopening; rerun alone, unchanged, **filled** in 142 s, read back on a fresh load (24 s). |
