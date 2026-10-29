# Segnalazione certificata per l'agibilità: generated, never walked

After the CILA rehearsal's works: the architect is the direttore dei lavori and attests, the
client is the tenant. Made-up data (an invented CILA, fine lavori and DOCFA, made-up
dichiarazione di conformità in `files/`); the draft is "PROVA NON INVIARE" and stops before
the declarations, signing and sending.

`portal-steps.generated.yaml` is `gen.py`'s, from `recipe.yaml`, the crawl of an empty draft
(`catalogue/F839/sections/agibilita.json`: 255 s, 117 choices) and `dictionary/fnsuap.yaml`.
The attestations (impianti, statics, energy, barriers, catasto, fire) are yes/no questions;
`supports` refuses works on structures, fire safety or house numbers, which aren't mapped.

## Runs

| Date | Browser | Login → draft | Result |
|---|---|---|---|
| 2026-10-28 | pane | 218 s of runs in six legs on one draft; attachments 9.5 s | 121 values and 4 attachments kept, every value read back on a fresh load (23.1 s) |

Each stop was general, fixed where it belongs, and the run resumed on the same draft:

1. A street's suggestion list open over "scala" (also once in the CILA): the runner now closes
   any open list before typing in a box.
2. "Attestazione tecnica - Agibilità": the portal marks the art. 24 c. 5-bis/5-ter and
   5-quater boxes as required, though they are exceptions (small or low units). Whether one
   applies is the direttore dei lavori's attestation: left to them (`theirs`, and a line in
   the person's steps).
3. "Sicurezza degli impianti": each system ticked opens its rows ("Subalt. n." and which
   certificate). The crawl tries no two choices together, so it never saw them: the
   dictionary names them by label inside the block that starts at the system's box, and the
   runner finds a moved id again by its label.
4. The label reader took "Il valore è obbligatorio" (the box's title and cell) for the label:
   it now skips validation messages in front.

Left to the person: the procura and the "Quadro riepilogativo - Agibilità" (portal documents,
signed), the art. 24 box above, the declarations, signing and sending.

## Clean reruns (2026-10-29)

In the pane, from run.py's install.js and run.js, nothing touched between runs: first filled in 89.3 s; second, with the runner waiting for a section to open, **filled** in 101.3 s, read back on a fresh load.
