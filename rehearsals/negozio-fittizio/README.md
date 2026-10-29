# A non-food shop's opening, generated from the batch crawl

"Aprire l'attività" in "Vicinato - settore merceologico NON alimentare" on Napoli's SUAP (the SCIA
per l'esercizio di vicinato), for a made-up ditta individuale, Bottega Fittizia di Carmela
Fittizia (`fixture.json`: a stationery and gift shop at via Luca Giordano 50; codice fiscale and
partita IVA pass the checksums and belong to nobody). The draft is "PROVA NON INVIARE - negozio
generato" and stops before the declarations, payment, signing and sending.

- **Crawl**: `catalogue/F839/sections/suap-016-aprire-l-attivita.json` (batch). Every section but
  one was the hairdresser's or the restaurant's by fingerprint.
- **Its own section**, "Dati sull'esercizio di vicinato": drafted by `draft.py` from
  `mod_esercizio_vicinato_apertura` (`schema/`), then written by hand: of the 25 boxes it
  drafted, 19 were right and 6 wrong (the season's and temporary dates, the C.C.N. delibera, a
  Si/No), all in its `check` part; the 14 it left were Napoli's own boxes (C.C.N., factory
  outlet, shopping-centre types). The same entry fills the food shop's opening (sector 017).
- **Questions**: the extras the wizard offers (fire prevention over 400 m², an outside sign,
  alcohol, weapons and the other special goods), a shopping centre and a factory outlet are
  refused at pre-flight; the fees are the 2026 table's €100.

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Stopped in the shop's section: three Si/No pairs in a row, and the runner counted a "Si" as a "No" (its nearby texts include the "No" before it), so the factory outlet's "No" was never ticked. `inpage.js` now counts a box by its own label, as the crawl does. Resumed on the draft from "Oggetto della pratica": every value kept and read back on a fresh load (57 s). The Scheda anagrafica was read back after its own Salva in the first pass. |

The attachments screen (read after the run): the fee receipt takes only signed files (`.p7m`),
so it's the person's; "Allegati liberi" is optional.
