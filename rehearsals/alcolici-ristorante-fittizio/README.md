# A running restaurant starts selling alcohol, generated from the batch crawl

"Comunicazione all'Agenzia delle Dogane per la vendita o somministrazione di prodotti alcolici"
(D.Lgs. 504/1995) for a bar or restaurant outside the protected zones on Napoli's SUAP, for the
pizzeria rehearsal's made-up ditta individuale (`rehearsals/pizzeria-fittizia/fixture.json`),
already running under its SCIA and adding wine and beer. The draft is "PROVA NON INVIARE -
alcolici ristorante generato" and stops before the statements, signing and sending.

- **Crawl**: `catalogue/F839/sections/suap-060-comunicazione-all-agenzia-delle-dogane-per-la-vend.json`
  (batch). The same section serves shops (both sectors) and hotels.
- **Its own section**: the products (a checklist), and the title the business runs under with
  the day it starts selling. The two statements (no convictions for illegal alcohol production;
  starting only once the notice is filed) are the person's.
- **Not covered**: alcohol in a new business's own opening. The wizard adds it with an
  "interventi eventuali" tick, which the batch doesn't try; pre-flight refuses it here.
- **Questions**: the products, the start date, the title and a short ATECO description (70
  characters). The portal's list of titles has no DIA.

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Stopped in the wizard: the recipe named the operation "...per la vendita di alcolici", the tree says "...per la vendita o somministrazione di prodotti alcolici". Fixed and resumed on the draft: every value kept and read back on a fresh load (88 s). |

The attachments screen has only "Allegati liberi" (optional), and no fee receipt slot, unlike
every other pratica run so far: the notice may carry no SUAP fee. The fees are still asked.
