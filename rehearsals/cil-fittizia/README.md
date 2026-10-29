# CIL for temporary works: generated, never walked

The Comunicazione di Inizio Lavori for opere temporanee (art. 6 c. 1 e-bis DPR 380/2001) on
Napoli's SUE, for the CILA rehearsal's people (`rehearsals/cila-fittizia`): the architect with
a procura, the tenant client, one firm. Made-up data; the draft is "PROVA NON INVIARE" and
stops before the declarations, signing and sending.

`portal-steps.generated.yaml` is `gen.py`'s, from `recipe.yaml`, the crawl of an empty draft
(`catalogue/F839/sections/cil.json`: 176 s, 79 choices) and `dictionary/fnsuap.yaml`. Its
quadri are the CILA's under other letters (b = b, d = e, f = h, g = i), matched without the
letter. New in the dictionary: quadro c) (the dates and what the works are) and the empty
"altre comunicazioni". The safety quadro shows the "under 200 uomini-giorno" choice as a
checkbox where the CILA has a radio (`kind: tick`), and the D.Lgs. 81 and notifica choices are
now questions.

## Runs

| Date | Browser | Login → draft | Result |
|---|---|---|---|
| 2026-10-28 | pane | 112.2 s to the end of the form (wizard 10.4, 15 sections, fresh load 14.9), then 14.5 s for 3 attachments | First run, no stop: 106 values and 3 attachments kept, every value read back on a fresh load |

Left to the person: the procura and the "Quadro riepilogativo della documentazione C.I.L."
(portal documents, signed), the declarations, signing and sending. Diritti: 100 € (line SUE 14
of the Comune's 2026 table, in the library's CIL page); the runs below filled 0 €, so the
"sono dovuti oneri" branch hasn't been walked yet.

## Clean reruns (2026-10-29)

In the pane, from run.py's install.js and run.js, nothing touched between runs: first filled in 76.1 s; second, with the runner waiting for a section to open, **filled** in 87.9 s, read back on a fresh load.

## Owner case (2026-10-29)

`answers-proprietario.json`: an usufruttuario holding the title alone, works on parts shared by
co-owners, done in proprio (D.Lgs. 81 doesn't apply), no co-holders' ID slot. In the pane,
after the dictionary mapped every title and firm case: **filled** in 59.7 s, 17 sections, fresh
load 11 s.
