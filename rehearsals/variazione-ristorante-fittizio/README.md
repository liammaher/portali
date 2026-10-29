# A bar or restaurant's new name, generated from the batch crawl

"Variazioni soggetti" for a bar or restaurant outside the protected zones on Napoli's SUAP, for
the pizzeria rehearsal's made-up ditta individuale (`rehearsals/pizzeria-fittizia/fixture.json`),
whose name changed: the vault holds the new name, the old one is asked. The draft is "PROVA NON
INVIARE - variazione ristorante generato" and stops before the declarations, signing and sending.

- **Crawl**: `catalogue/F839/sections/suap-060-variazioni-soggetti.json` (batch).
- **Its own sections**: the changes (a new name or a moved registered office, for a ditta
  individuale; every box is shown, the company changes stay empty) and the title the business
  runs under. The hairdressers' version of the changes (each tick opens its boxes) is mapped too;
  the shops' version opens boxes the crawl didn't reach (two levels deep), so it isn't.
- **Questions**: shares, a legal representative, partners or a new professional manager are
  refused at pre-flight. The fees are asked (the 2026 table's VPF column; this run used "non
  dovuti").

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Filled with no stop: every value kept and read back on a fresh load (71 s). |

The attachments screen: the fee receipt takes only signed files, so it's the person's;
"Allegati liberi" is optional.
