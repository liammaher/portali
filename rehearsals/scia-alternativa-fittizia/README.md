# SCIA alternativa al permesso di costruire (art. 23 DPR 380/2001)

The SCIA alternativa for the CILA rehearsal's job: the architect with a procura designs and
presents, for the tenant client, with the owner company's assent, two firms, and Davide Esempio
as structural designer (`s:`). The P6 case: an art. 10 c. 1 c ristrutturazione edilizia (new
facades and openings, more volume, structural strengthening), the contributo on the architect's
prospetto paid in instalments, under the energy rules (its report attached), with the seismic
authorization asked. Made-up data (rehearsals/cila-fittizia's people and property); the draft is
"PROVA NON INVIARE" and stops before the declarations, payment, signing and sending. The works
start 30 days after filing.

`portal-steps.generated.yaml` is `gen.py`'s, from `recipe.yaml`, the crawl of an empty draft
(`catalogue/F839/sections/sue-segnalazione-certificata-inizio-attivita-alternativa-al-permesso-di-cost.json`:
37 sections) and `dictionary/fnsuap.yaml`.

## What it maps

- **What it files** (quadro c) and the asseverazione's tipologia): an art. 10 c. 1 c
  ristrutturazione edilizia, or a new building (or ristrutturazione urbanistica) under a piano
  attuativo, or directly under the town plan, with precise plano-volumetric rules; each changing
  the use or not (the SCIA's four cases).
- **The contributo** (quadro g): free (the rule why), or due: on the architect's prospetto or by
  the SUE, paid within 30 days of filing, in instalments or during the works. No opere a scomputo.
- **The rest is the SCIA's**: six of its asseverazioni are the same sections (the dictionary's
  SCIA entries, aliased), the structural designer's block, every protection, the fasce. Its own:
  the stato legittimo without the sanatoria question; the systems with the energy report
  attached and the renewables; acoustics, excavated earth and fire prevention asked as on the
  permesso; soil, urbanisation and drains; the other ecological vincoli as checkboxes.
- **The attachments screen**: the co-holders' assent and IDs and the energy report (plain files,
  required once their answers apply) are filled; the stato legittimo documents, the procura, the
  DSC and the receipt are the architect's (signed files only, or portal documents).

Refused at pre-flight (`supports`): a landscape authorization (its own operation: not mapped
yet); works not allowed in a cemetery's fascia; an acoustic impact or clima acustico report; a
project for the Vigili del Fuoco; asbestos; a hygiene derogation; the barriers rules; systems
needing a project; soil needing investigations; an area without its urbanisation; a contributo
already paid (its form has no receipt box). Not offered: the 4th tipologia (regional works).

## Runs

| Date | Browser | Case | Result |
|---|---|---|---|
| 2026-10-29 | pane | P6 (plain), first legs on one draft | sections 0-19 filled first time; stopped on the description box (named by its counter: now by "e che consistono in"), then on "specificare il tipo di intervento" (the 4th tipologia's: left), then every asseverazione filled and read back on a fresh load (32 s); stopped on the attachments screen's required slots (read and recorded in the crawl, the energy report mapped) |
| 2026-10-29 | pane | P6 (plain), from scratch, in a batch after the permesso | **filled** in 194 s with 9 attachments; fresh load 33 s, every value read back; no retry |
