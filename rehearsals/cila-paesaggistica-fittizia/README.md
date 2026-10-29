# CILA with an autorizzazione paesaggistica semplificata, one pratica: generated, run to the declarations

The CILA rehearsal's P6 job (a tenant's interior works, already started, two firms) with an
outdoor air-conditioning unit (Allegato B item B.7) on a building under art. 136 and 142: the
CILA and the landscape authorization in one pratica on Napoli's SUE. Made-up data; the draft is
"PROVA NON INVIARE" and stops before the declarations, the attachments, payment, signing and
sending.

- **Two operations** (`operation:` a list). gen.py takes each one's sections from the catalogue
  with the procura scelta, and the pratica is their union, in the order of the combination's
  crawl (`catalogue/F839/sections/sue-combo-cila-paesaggistica-semplificata.json`, fingerprints
  read on an empty draft). It refuses a crawl that isn't of the operations together. A section
  both have under two names (the semplificata's "ubicazione dell'intervento" is the CILA's
  "Localizzazione") is shown once, so it is filled once.
- Every section is already in the dictionary: the CILA's, plus the semplificata's tecnico,
  Allegato B, vincolo and bollo.
- `supports`: the CILA's (a tenant with the owner's assent) and the semplificata's (no variant,
  no marche da bollo).
- No Allega documenti step yet: the combination's attachments screen isn't read (`slots`).

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-29 | pane | Stopped at the firms (h): with `nth: 2` the runner counted the "Ragione sociale" box as a "codice fiscale" (the next column's label leaks into its texts), so the legale rappresentante's CF went into the firm's partita IVA. Fixed in inpage.js: `nth` counts only the boxes that match at the best place. |
| 2026-10-29 | pane | **Filled**, resumed on the same draft: all 22 sections read back on a fresh load. The ordinaria was then re-run (both deed registrations: the second and third "con sede a", "al n. gen.", "in data"): filled. |
