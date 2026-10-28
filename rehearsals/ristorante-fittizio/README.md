# A restaurant's opening, generated from the batch crawl

"Aprire l'attività" for a bar or restaurant outside the protected zones on Napoli's SUAP: the
SCIA with the Reg. 852/2004 notifica, for the pizzeria rehearsal's made-up ditta individuale
(Pizzeria Fittizia di Gennaro Fittizio, `rehearsals/pizzeria-fittizia/fixture.json`; codice
fiscale and partita IVA pass the checksums and belong to nobody). The draft is "PROVA NON
INVIARE - ristorante generato" and stops before the declarations, payment, signing and sending.

How its steps file was made (`portal-steps.generated.yaml`, by `gen.py`):

- **Crawl**: the batch crawl (`crawl.js` batch, on the scratch draft "catalogo SUAP"):
  `catalogue/F839/sections/suap-060-aprire-l-attivita.json`. 19 sections, 4 of them new
  (the rest the hairdresser's, by fingerprint).
- **Its own sections** (the restaurant's data, the 852 notifica's start and activity list):
  drafted by `draft.py` from two national forms (`mod_somministrazione_zone_non_tutelate_apertura`
  and `mod_notifica_sanitaria`), checked by hand against the pizzeria's walk
  (`rehearsals/pizzeria-fittizia/portal-steps.yaml`) and put in the dictionary. The activity
  list maps only the food-service block (bar, restaurant, canteen); `supports` refuses the rest.
- **Questions**: the pizzeria's (historic centre, alcohol, music, outside sign refused at
  pre-flight), plus the 852 activity as a checklist.

`gen.py --check` against the pizzeria's hand walk: the same ids where both fill a box; the
generated file also fills the oggetto, scala and piano, and has the branches the walk didn't take
(season, shopping centre, a later start, a temporary activity, other food-service lines).

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Stopped at "Superficie di somministrazione": the box's title and cell both say "mq", which took two of the three texts the runner reads, so the label beside it wasn't seen. `inpage.js` now counts a repeated text once. Resumed on the same draft: 53 values in 10 sections, every one read back on a fresh load (48 s, then 25 s). |

The attachments screen (read after the run): the planimetria, the ASL tariff receipt and the
fee receipt take only signed files (`.p7m`), so they're the person's; "Allegati liberi" is
optional.
