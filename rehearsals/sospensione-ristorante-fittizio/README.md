# Suspending a bar or restaurant, generated from the batch crawl

"Sospendere l'attività" for a bar or restaurant outside the protected zones on Napoli's SUAP (the
suspension with the Reg. 852/2004 update), for the pizzeria rehearsal's made-up ditta individuale
(`rehearsals/pizzeria-fittizia/fixture.json`), closed for the winter. The draft is "PROVA NON
INVIARE - sospensione ristorante generato" and stops before the declarations, signing and sending.

- **Crawl**: `catalogue/F839/sections/suap-060-sospendere-l-attivita.json` (batch).
- **Its own sections**: the suspension's "Oggetto della comunicazione"
  (`rehearsals/sospensione-negozio-fittizio`) and the 852 suspension: the registration's protocol
  and the same dates.
- **Questions**: the dates, the titles; the fees are asked (see the shop's README).

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Filled with no stop: every value kept and read back on a fresh load (128 s, the portal slow). "Prosegui" then stayed on the form instead of opening the payment screen, unlike the shop's; not looked into (the runner's frame was closed for the batch), so the attachments screen isn't read and the steps stop at the form. |
