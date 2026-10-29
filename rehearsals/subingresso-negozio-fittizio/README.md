# Taking over a non-food shop, generated from the batch crawl

"Subentrare nell'esercizio dell'attività" in "Vicinato - settore merceologico NON alimentare"
on Napoli's SUAP (the comunicazione di subingresso), for the shop rehearsal's made-up ditta
individuale (`rehearsals/negozio-fittizio/fixture.json`) buying an existing shop. The draft is
"PROVA NON INVIARE - subingresso negozio generato" and stops before the declarations, payment,
signing and sending.

- **Crawl**: `catalogue/F839/sections/suap-016-subentrare-nell-esercizio-dell-attivita.json` (batch).
- **Its own sections** are the subingresso's, the same in every sector, written by hand from the
  crawl and `mod_subingresso`: the activity and the title it ran under, the start date; how it
  changes hands and the registered deed; a new web address (left empty). With them the
  subingresso is filled in ten sectors (shops, food shops, bars and restaurants in both zones,
  hairdressers, beauticians, hotels, food labs).
- **Questions**: fire prevention and a deed still being registered (the portal then takes its
  signed copy, the person's) are refused at pre-flight; the fees are the 2026 table's €80 (S1).

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Filled with no stop: every value kept and read back on a fresh load (46 s). |

The attachments screen: the fee receipt takes only signed files, so it's the person's;
"Allegati liberi" is optional.
