# Rooms to let (affittacamere), generated from the batch crawl

"Aprire l'attività" in "Strutture ricettive extralberghiere" on Napoli's SUAP (the SCIA for
affittacamere, B&B and holiday homes), for a made-up ditta individuale, Camere Fittizie di Anna
Fittizia (`fixture.json`: three double rooms, two with a bathroom, no food, at via Francesco
Cilea 100; codice fiscale and partita IVA pass the checksums and belong to nobody). A B&B is
the same pratica with its own two ticks; the business form fits rooms to let. The draft is
"PROVA NON INVIARE - affittacamere generato" and stops before the declarations, payment,
signing and sending.

- **Crawl**: `catalogue/F839/sections/suap-056-aprire-l-attivita.json` (batch).
- **Its own sections**, written by hand from the crawl and
  `mod_attivita_extra_alberghiere_apertura`: type and form, name, season, services (no pool,
  beauty centre, gym or shop), and the rooms: singles and doubles, each row's totals and the
  structure's are sums of the four counts asked (`{{ask.a+2*ask.b}}`, new in `run.py`).
- **Questions**: more than 25 beds or fire prevention, alcohol, an outside sign, those services
  and any food for the guests are refused at pre-flight; the fees are the 2026 table's €200 (S94).

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Two stops, both the runner's: "Via Francesco Cilea" was searched by its longest word, and "Francesco" fills the portal's 20-street list without Cilea (now the next word is tried); and the rooms and beds totals share a header row, so the beds went into the rooms box (now a box's own title wins). Resumed on the draft: every value kept and read back on a fresh load. |

The attachments screen (read after the run): the CURS data sheet is a document the portal makes
(download, fill, sign), the fee receipt takes only signed files, so both are the person's; the
floor plan signed by a technician takes a plain PDF too, but it's the technician's; "Allegati
liberi" is optional.
