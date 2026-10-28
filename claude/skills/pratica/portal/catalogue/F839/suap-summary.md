# Napoli's SUAP wizard: first pass (operations only)

Crawled 2026-10-28 by `wizard.js` with `follow: false` on the scratch draft "PROVA NON INVIARE -
catalogo SUAP", in 403 s: every sector's operations tree, no pratica generated yet.

- **105 sectors** (Agricoltura … Intrattenimento, plus "Ambiente, Altri adempimenti" and
  "Sicurezza alimentare e sanità pubblica veterinaria (GISA)").
- **283 distinct operations** (by type and label path).
- **64 operations offered in at least 90% of sectors**: AUA and the other environmental
  permits, fire prevention (PIN 1–7, gpl), occupazione suolo pubblico (dehors, tende, events),
  mezzi pubblicitari (insegne, impianti, teli), GISA notices and recognitions, the Reg. 852
  food-business notifica.
- **6 to 42 operations specific to each sector**: open, take over, move, suspend, close the
  activity, structural changes, changes of people, and the sector's own (e.g. for a
  restaurant outside the protected zones: Aprire l'attività, Subentrare, orario in deroga,
  Trasferire, Cessare, Dogane for alcohol, Sospendere, Ampliamento di superficie, Variazione
  del preposto, Variazioni soggetti, plus the food-safety operations).

The full data (every sector's operations as label paths) is still only in the browser pane's
storage (`localStorage['catalogue.F839.suap.tree']`, 67 KB) until it's exported here as
`suap-wizard.json`.

## Next

Generate the pratica for each operation, as `sue-wizard.json` does for the SUE: the 64 common
ones once, and the sector-specific ones per sector. At 8–15 s each that is about 2,100
operations, several hours, so by priority: restaurants and bars, shops (vicinato), B&B and
extralberghiere, acconciatori/estetisti, then the rest.
