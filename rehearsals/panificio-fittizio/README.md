# A bakery's opening, generated from the batch crawl

"Aprire l'attività" in "Panificio" on Napoli's SUAP (the SCIA di panificazione with the Reg.
852/2004 notifica), for a made-up ditta individuale, Panificio Fittizio di Salvatore Fittizio
(`fixture.json`: bread and pizza sold over the counter at via Foria 120; codice fiscale and
partita IVA pass the checksums and belong to nobody). The draft is "PROVA NON INVIARE -
panificio generato" and stops before the declarations, payment, signing and sending.

- **Crawl**: `catalogue/F839/sections/suap-004-aprire-l-attivita.json` (batch).
- **Its own sections**, written by hand from the crawl and `mod_panifici_apertura`: the bakery's
  data (eating on the spot, a C.C.N., a shopping centre, the season) and who runs production (a
  fact; the antimafia statement and what the manager guarantees are the person's to tick). The
  852 activity list gained the bakery lines and food retail in a shop.
- **Questions**: ovens or heating over 116 kW, more than 300 kg of flour a day, underground
  premises and an outside sign are refused at pre-flight. The fees are asked: the 2026 table's
  bakery line isn't in the library (with fire prevention it is €140, S155); this run used €120.

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Stopped at the address: "Via Foria" is also inside "Via Santa Maria Avvocata a Foria". `inpage.js` now takes the street named exactly. Resumed on the draft: every value kept and read back on a fresh load. |

The attachments screen (read after the run): the ASL tariff receipt and the fee receipt take
only signed files (`.p7m`), so they're the person's; "Allegati liberi" is optional.
