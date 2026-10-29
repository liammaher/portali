# Autorizzazione paesaggistica semplificata: generated from the batch crawl, run to the declarations

On Napoli's SUE, for the CILA rehearsal's architect (with procura) and client: new windows in
another colour and an outdoor air-conditioning unit (Allegato B items B.3 and B.7) on a property
under both art. 136 (an invented D.M.) and art. 142 c.1 a). Made-up data; the draft is
"PROVA NON INVIARE" and stops before the declarations, the attachments, the bollo, signing
and sending.

How its steps file was made (`portal-steps.generated.yaml`, by `gen.py`):

- **Crawl**: the SUE batch: `catalogue/F839/sections/sue-autorizzazione-paesaggistica-semplificata.json`,
  14 sections, all but two seen in other pratiche. The batch tried 30 of Qualificazione's
  boxes; the rest were tried in the pane on 2026-10-29 (B.22 by hand: crawl.js never ticks a
  box with "autorizz" in it). None of the 42 opens or moves anything.
- **Its own two sections**, written by hand into the dictionary:
  - *Qualificazione dell'intervento*: one box per item of DPR 31/2017's Allegato B, asked as
    one checklist (`allegato_b`); works may fall under several. It shares its name with the
    SCIA's section of one radio: gen.py takes the entry that fits.
  - *Ambito dell'intervento*: the protection as one answer (`vincolo`: art. 136, art. 142,
    both, or art. 134 alone). Ticking art. 136 opens its letter and the declaring D.M. above
    the box itself, so the box renumbers (r2c1p2 → r3c1p3) and goes by label, as does what
    follows it; each case is its own branch.
- `supports` refuses a variant of an issued authorization and the marche da bollo route.

What's the architect's, in `yours`: the relazione paesaggistica semplificata (Allegato D), the
drawings and photos, the signed procura, the bollo and diritti, signing and sending.

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Not run: impresainungiorno answered 503 (maintenance) at 23:50. Pre-flight ready with `answers-p6.json` and the P6 fixture's values. |
| 2026-10-29 | pane | Stopped at Ambito: the art. 136 tick renumbered itself, which gen.py didn't yet send by label (fixed: `Section.moves_itself`). |
| 2026-10-29 | pane | **Filled**, a new draft: 12 sections in 54 s (wizard 9 s), every value read back on a fresh load of the draft (8 s). Run with art. 136 and 142 together; 136 alone and 134 alone not run. |
