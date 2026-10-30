# Segnalazione certificata per l'agibilità: generated, never walked

After the CILA rehearsal's works: the architect is the direttore dei lavori and attests, the
client is the tenant. Made-up data (an invented CILA, fine lavori and DOCFA, made-up
dichiarazione di conformità in `files/`); the draft is "PROVA NON INVIARE" and stops before
the declarations, signing and sending.

`portal-steps.generated.yaml` is `gen.py`'s, from `recipe.yaml`, the crawl of an empty draft
(`catalogue/F839/sections/agibilita.json`: 255 s, 117 choices) and `dictionary/fnsuap.yaml`.
The attestations (impianti, statics, energy, barriers, catasto, house numbers, fire, lifts)
are pre-flight's questions.

## What it covers (2026-10-30)

- **Works on structures** (art. 24 c. 5 b) DPR 380/2001): the collaudo statico certificate of
  art. 67, or for repairs and local works the direttore dei lavori's dichiarazione di regolare
  esecuzione (art. 67 c. 8-bis, 8-ter), each attached or named by where it is kept, protocol and
  date; or minor works with no collaudo (the form's own box). Each "references" choice has its
  own rows with the same labels: they go by the block that starts at that choice.
- **Partial agibilità** (art. 24 c. 4 a) and b)): single buildings or single units, in both the
  presentation and the attestation.
- **SCIA Unica** (art. 19-bis c. 2 L. 241/1990): proposed when the fire SCIA (art. 4 c. 1 DPR
  151/2011) goes with it; the presentation's second row of radios.
- **Energy**: the AQE attached. **Barriers**: art. 77 or art. 82. **House numbers**: the request
  attached, or the one already filed by protocol and date. **Lifts**: this agibilità counts as
  the communication of art. 12 DPR 162/1999, or names the one filed before.
- **Every system** the form lists (ten, "Altro impianto" with what it is), each with its
  dichiarazione di conformità or di rispondenza. A system certified otherwise (a collaudo, an
  atto notorio, a document already filed with the Comune) is refused at pre-flight.

Each "si allega" adds its own required slot on Allega documenti (read on the portal
2026-10-30): Certificato di collaudo statico, Dichiarazione di regolare esecuzione, Attestato
di qualificazione energetica (AQE), Documentazione necessaria per l'assegnazione o
aggiornamento di numerazione civica, SCIA ai sensi dell'art. 4 comma 1 del d.P.R. n. 151/2011,
and for a partial agibilità the Copia di elaborato planimetrico (the plan marking the works
completed). The recipe fills them (`on_page`).

## Runs

| Date | Browser | Login → draft | Result |
|---|---|---|---|
| 2026-10-28 | pane | 218 s of runs in six legs on one draft; attachments 9.5 s | 121 values and 4 attachments kept, every value read back on a fresh load (23.1 s) |
| 2026-10-29 | pane | 89.2 s from a blank draft (`bench/rerun.py` batch, after the SCIA's dictionary change; fresh load 14.3 s) | **Filled**, every value read back on a fresh load |
| 2026-10-30 | pane | `answers-collaudo.json`: 96.4 s from a blank draft (fresh load 16.1 s) | **Filled**: collaudo attached, AQE, house-number request, fire SCIA (SCIA Unica), all ten systems, the lift's communication; 8 files kept |
| 2026-10-30 | pane | `answers-estremi.json`: first try stopped on a required slot the partial agibilità adds (the elaborato planimetrico), mapped; rerun 87.7 s (fresh load 13.6 s) | **Filled**: regolare esecuzione, house-number request and lift communication by their references, one unit's partial agibilità, art. 77; 5 files kept |
| 2026-10-30 | pane | `answers-parziale.json`: 89.9 s (fresh load 14.3 s) | **Filled**: the collaudo certificate by its references, a building's partial agibilità, art. 82 |
| 2026-10-30 | pane | `answers-p6.json` on the final steps: 87.6 s (fresh load 13.4 s) | **Filled**, unchanged |

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
