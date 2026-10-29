# Taking over a bar or restaurant, generated from the batch crawl

"Subentrare nell'esercizio dell'attività" for a bar or restaurant outside the protected zones on
Napoli's SUAP (the subingresso with the Reg. 852/2004 update), for the pizzeria rehearsal's
made-up ditta individuale (`rehearsals/pizzeria-fittizia/fixture.json`) renting an existing
pizzeria (affitto d'azienda) from a made-up previous owner whose codice fiscale passes the
checksum and belongs to nobody. The draft is "PROVA NON INVIARE - subingresso ristorante
generato" and stops before the declarations, payment, signing and sending.

- **Crawl**: `catalogue/F839/sections/suap-060-subentrare-nell-esercizio-dell-attivita.json` (batch).
- **Its own sections**: the subingresso's (see `rehearsals/subingresso-negozio-fittizio`), the
  previous holder's 852 notifica, and the 852 start date, the opening's.
- **Questions**: a takeover in the historic centre and a deed still being registered are
  refused at pre-flight; the fees are the 2026 table's €96 (S72).

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Filled with no stop: every value kept and read back on a fresh load (50 s). |

The attachments screen: the fee receipt and the ASL tariff receipt take only signed files, so
they're the person's; "Allegati liberi" is optional.
