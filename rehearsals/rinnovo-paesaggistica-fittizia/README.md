# Rinnovo dell'autorizzazione paesaggistica semplificata: generated from the batch crawl, run to the declarations

On Napoli's SUE (DPR 31/2017 art. 7): the renewal, without changes, of an invented semplificata
issued in 2021. Made-up data; the draft is "PROVA NON INVIARE" and stops before the declarations,
the attachments, the bollo, signing and sending.

- **Crawl**: `catalogue/F839/sections/sue-rinnovo-autorizzazione-paesaggistica-semplificata.json`;
  its own section is *Qualificazione dell'intervento di rinnovo*: the expired authorization (number,
  date, expiry, ente) and whether it was a semplificata or an ordinaria.
- A renewal of an ordinaria that changes the project opens more boxes, not mapped: only "without
  changes" is supported (`paes_rinnovo_variazioni`, default no).

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-29 | pane | **Filled** on the first try: 11 sections in 48 s, every value read back on a fresh load. |
| 2026-10-29 | pane | Clean reruns of all 23 SUE recipes from run.py's install.js and run.js, nothing touched between runs: first filled in 54.3 s; second, with the runner waiting for a section to open, **filled** in 53.8 s, read back on a fresh load. |
