# Test: can a pratica nobody walked be filled from the crawl alone?

The Comunicazione di fine lavori on Napoli's SUE (impresainungiorno), for the works of the CILA
rehearsal's P6 (`rehearsals/cila-fittizia`): the same architect with a procura, the same
tenant client, the same owner company. Made-up people (the CILA's `fixture-p6.json`, through
the same synthetic values); the draft is marked "PROVA NON INVIARE" and stops before what is
the person's: the declarations, signing and sending.

Nobody walked this pratica's screens. Its steps file, `portal-steps.generated.yaml`, comes
from `claude/skills/pratica/portal/gen.py`:

- `recipe.yaml`: the operation and scelta, the person's own steps, the cases it supports.
- `catalogue/F839/sections/fine-lavori.json`: `crawl.js` on an empty draft (134 s: 13 sections,
  57 choices tried, the attachments screen's 7 slots).
- `dictionary/fnsuap.yaml`: what goes in each field, by section and label, shared with the
  CILA.

Of its sections, seven are the CILA's (Scheda anagrafica, procuratore, oggetto, the two
domicili, diritti) and two are the CILA's under other names, with the same fields and ids:
"ubicazione dell'immobile" (Localizzazione) and "Titolarità dell'intervento" (quadro a). One
is new: "comunicazione di fine lavori" (the date, whole or in part, the title the works ran
under and its protocol). Adding it to the dictionary was the only knowledge this pratica
needed: six lines.

`answers-p6.json` holds the made-up answers: works ended 27/10/2026, completely, under a
C.I.L.A. with an invented protocol (PG/2026/0999999 of 20/10/2026), no diritti (0 €: whether
Napoli's tariff charges for a fine lavori is the person's answer, not known here).

## Runs

| Date | Browser | Login → draft | Result |
|---|---|---|---|
| 2026-10-28 | pane | 60.3 s to the end of the form (wizard 11.7, 9 sections ~40, fresh load 7.8), then 5.4 s for the attachments | First run, no stop: 70 values and 3 attachments kept, every value read back on a fresh load |

The attachments went in a second step: `gen.py` refuses an attachments screen nobody has seen,
so the first run ended with the form; the slots were read on the screen (`crawl.js` slots()),
the steps generated again, and the run resumed on the same draft. Left to the person: the
portal's procura and its "Quadro riepilogativo della documentazione - Fine lavori" (both
portal documents, signed), the declarations, signing and sending.

## What this shows, and what it doesn't

- A pratica of the same family is a crawl (2 min), at most a few dictionary lines, and a
  recipe; not a walk.
- One run on one day, with made-up data. Accepted at sending is not tested: the stop point is
  before it.
- Supported: a tenant (title "altro" + locatario, the owner's assent), works finished
  completely. A partial fine lavori needs a planimetria, not mapped; pre-flight refuses it.

## Clean reruns (2026-10-29)

In the pane, from run.py's install.js and run.js, nothing touched between runs: first filled in 60 s; second, with the runner waiting for a section to open, **filled** in 55.1 s, read back on a fresh load.
