# SCIA edilizia (manutenzione straordinaria): generated, never walked

The SCIA for the CILA rehearsal's works filed as a SCIA: the architect with a procura designs
and presents, the tenant client, the owner company, two firms. Made-up data (an invented
1965 licenza, PRG zone and article); the draft is "PROVA NON INVIARE" and stops before the
declarations, signing and sending.

`portal-steps.generated.yaml` is `gen.py`'s, from `recipe.yaml`, the crawl of an empty draft
(`catalogue/F839/sections/scia.json`: 764 s, 36 sections, 319 choices) and
`dictionary/fnsuap.yaml`. 32 sections, 209 fields, 49 questions. The progettista's twelve
asseverazione sections are the plain case only (manutenzione straordinaria, the building's
own title, no protection, structures, fire, acoustics or asbestos); `supports` refuses the
rest. The wizard's required Si/No (sanatoria) is answered No (`si_no` in the recipe).

## Runs

| Date | Browser | Login → draft | Result |
|---|---|---|---|
| 2026-10-28 | pane | several legs on one draft (one per stop below); fresh load 38.8 s; attachments 6.6 s | 198 values and 7 attachments kept, every value read back on a fresh load |

What the stops taught (each fixed for every pratica, then resumed on the same draft):

1. The wizard reported "filled" while the portal refused its CONFERMA (the required Si/No):
   its screen check read the whole page, whose menu lists every screen. It now checks the
   title and answers Si/No questions (`answers`).
2. The licence number box takes 8 characters: the crawler now records each box's maximum
   and `run.py` refuses a longer value before the run.
3. The safety quadro's "e allega alla presente segnalazione la notifica" shows only with the
   notifica answer: `on_page` in the dictionary (by label, skipped where absent).
4. Surface and volume came back as "85.00": decimal boxes are compared as numbers.
5. After a resume the fresh-load check read a moved id: read-back now finds it by label too.
6. One regression of mine, caught by the run: a label-reader change let the box to the left's
   label count (the firms' block). Narrowed to skip only leading validation messages.

Attachments: the "Documentazione dimostrativa dello stato legittimo" slot takes only signed
files (`.p7m`), so it is the person's; `gen.py` now leaves every signed-only slot to them.
Also the person's: the procura and the "Quadro riepilogativo - SCIA DSC" (portal documents),
naming the direttore dei lavori (the crawl doesn't reach "di aver incaricato"'s fields), the
declarations, signing and sending.
