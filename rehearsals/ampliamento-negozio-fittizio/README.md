# A non-food shop's bigger sales area, generated from the batch crawl

"Variazioni strutturali" in "Vicinato - settore merceologico NON alimentare" on Napoli's SUAP,
for the shop rehearsal's made-up ditta individuale (`rehearsals/negozio-fittizio/fixture.json`),
going from 40 to 60 m² of sales area in the same sector. The draft is "PROVA NON INVIARE -
ampliamento negozio generato" and stops before the declarations, payment, signing and sending.

- **Crawl**: `catalogue/F839/sections/suap-016-variazioni-strutturali.json` (batch).
- **Its own section**, "dati sull'entità delle variazioni": the title, the area before and
  after, per sector and in all. The food shop's version (special goods rows shown) is mapped too.
- **Questions**: fire prevention, a change of sectors or special goods and an outside sign are
  refused at pre-flight. The fees are asked (this run used €80, the VCA line S1: to confirm).
- The form's protocol box takes 10 characters: `run.py page` refused "PG/2026/0666666" before
  opening anything, and pre-flight now asks such an answer again.

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Filled with no stop: every value kept and read back on a fresh load (78 s). |

The attachments screen: the fee receipt takes only signed files, so it's the person's;
"Allegati liberi" is optional.
