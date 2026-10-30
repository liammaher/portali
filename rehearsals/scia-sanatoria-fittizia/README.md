# SCIA in sanatoria: generated, never walked

The SCIA rehearsal's job (rehearsals/scia-fittizia: new shutters and railings of another design on
the facade, two partitions, a bathroom, the systems) found done without a SCIA. Same people and
made-up data; the draft is "PROVA NON INVIARE" and stops before the declarations, signing and
sending.

`portal-steps.generated.yaml` is `gen.py`'s, from `recipe.yaml`: the SCIA's crawl
(`catalogue/F839/sections/scia.json`, which tried the wizard's sanatoria scelta), `dictionary/fnsuap.yaml`,
and the wizard's "Le opere ricadono nell'ambito della Sanatoria ...?" answered Sì, which adds the
quadro "SANATORIA e regolarizzazioni per interventi già realizzati e in corso di esecuzione".

## What it covers (2026-10-30)

The SUE rules table (`rules/sue-napoli.yaml`) gives this pratica (`napoli/comune/scia-in-sanatoria`)
for works already done or under way that a SCIA needed: manutenzione straordinaria on the facades,
or a ristrutturazione without structures, with no change of use, outside protected areas and
bene culturale. Interior works only are the late CILA's.

- **In corso** (art. 37 c. 5 DPR 380/2001): works still under way, the SCIA filed of one's own
  accord with the €516 sanction, paid and given by its reference and date; the asseverazione's
  start date.
- **Doppia conformità** (art. 36-bis c. 5 b): works done, conforming to the planning and
  building rules both then and now; the date, the oblazione the technician determines (€516 to
  €5,164) paid and given by amount and date, in both quadri.
- The asseverazione: the stato legittimo also from this sanatoria, the SCIA "presentata in
  sanatoria", the catasto (unchanged, DOCFA filed, or to file once it takes effect). Works on
  structures are refused by the table.
- The sanatoria's own required slot, "Elaborati richiesti per la sanatoria (stato legittimo, stato
  di fatto e comparativi)", takes only signed files (.p7m): the technician's, in the person's steps.

Refused at pre-flight, with the reason:

- **Conformità urbanistica** (art. 36-bis c. 1, €1,032 to €10,328): on the portal the
  asseverazione's dating (by documents or by attestation), its oblazione and its works proposal
  (art. 36-bis c. 2) are one radio group though the form asks for all three: ticking one unticks
  the others (seen 2026-10-30), so the asseverazione can't state the case. Ask the SUE.
- **Variants to pre-1977 titles** (art. 34-ter): not mapped.
- Structures, protected areas, bene culturale, changes of use, a permesso's works: the table's.

The payment goes by its references: the receipt route has no slot of its own (it would go in the
fee slot, the person's .p7m). The Comune's SCIA form (23/06/2025) still says cases d.2 to d.4 go on
paper; DD 501/2026 makes the portal the only channel from 1/7/2026, and the portal offers them.

## Runs

| Date | Browser | Answers | Result |
|---|---|---|---|
| 2026-10-30 | pane | `answers-p6.json` (doppia conformità) | First try stopped on Tipologia: "presentata in sanatoria" ticked before the intervention type, whose id then landed on it (now ticked last, by label). Rerun from a blank draft: every section filled and read back on a fresh load (150.6 s, fresh load 24.7 s); then the sanatoria drawings slot turned out to take only .p7m (moved to the person's steps) and the attachments resumed on the same draft (7.9 s): **filled**, 7 files kept |
| 2026-10-30 | pane | `answers-in-corso.json` (art. 37 c. 5) | **Filled** from a blank draft in 143.4 s, read back on a fresh load (24.2 s), 7 files kept |

Exploring by hand first (a blank draft with only the wizard, then ticking each case) showed the
four cases' rows and the case-3 radio group. Opening the sanatoria quadro right after saving an
asseverazione section twice sent the portal back to its home page; opening it from the Scheda
anagrafica, and in both runs, worked.

The plain SCIA's P6 (rehearsals/scia-fittizia) on the steps regenerated with these entries: filled
in 140 s, read back on a fresh load (23.4 s).
