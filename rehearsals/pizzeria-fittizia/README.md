# Rehearsal: Pizzeria Fittizia opens in Fuorigrotta

The first pratica we take end to end ourselves, on the real portal with a made-up business, up to
the owner's taps. It's the north star's first milestone without a partner (`docs/north-star.md`,
"First milestone" and open question 6). Real forms, real websites, invented facts; the pratica is
never sent.

## The situation

Gennaro Fittizio, a pizzaiolo, opens a 20-seat pizzeria as a ditta individuale in a former shop at
viale Augusto 74, Fuorigrotta (60 m², catasto C/1, rented from 1 November 2026). A gas oven, so no
wood-oven flue; no spirits, no music. It copies a common Naples pattern (a pizzaiolo taking a shop
on a busy street), placed outside the protected zones on purpose.

Everything about it is in [`fixture.json`](fixture.json), as a business vault. The partita IVA and
codice fiscale pass their checksums and belong to nobody we know of.

**Why Fuorigrotta.** Since 25 July 2026 the Piano for the centro storico UNESCO and the buffer zone
puts an opening there under an authorization, not a SCIA. The Piano's street list
(`ELENCO-STRADE-PIANO-ZONE-TUTELATE.pdf`, checked 2026-10-27) has most of the Vomero (via Luca
Giordano, via Cimarosa, via Scarlatti, via Merliani) in zona A area 1. Viale Augusto isn't on it, so
it's zona C: a SCIA, and the pizzeria can open the day the receipt arrives. The vault places it in
Municipalità 10, Fuorigrotta, ASL Napoli 1 distretto 25 (`pvault locate --street "viale Augusto"`).

## The pratica

The SCIA unica on impresainungiorno-Napoli: somministrazione plus the ASL notifica sanitaria
(library: `napoli/comune/scia-somministrazione-alimenti-bevande` and
`napoli/asl-napoli-1/registrazione-attivita-alimentare`). The front office builds its own modules,
so this is web-form filling from vault facts, not a PDF layout.

The screens, box by box: [`portal-map.yaml`](portal-map.yaml) (L0 until walked).

Next in the chain, once this one holds: TARI non domestica by PEC (filled, never sent), then the
insegna and the dehors.

## Lines we don't cross

- **Never send.** An invented declaration sent to an office is a false statement (DPR 445/2000,
  art. 76). A draft isn't a declaration.
- **Never pay, and don't create a pagoPA position**: stop before "Procedi con il pagamento tramite
  pagoPA".
- **Delete the draft** after each walk (Pratiche in bozza).
- **Logins are the person's.** Liam logs in with his own SPID; Claude never types credentials.
- **No real third party's data.** The titolare is Gennaro Fittizio. If the portal ties the titolare
  to whoever is logged in, record it, and use Liam's own identity for that walk.

## How a walk runs

1. Serve the business vault, as the pratica skill would read it, with a scoped token:
   ```sh
   mkdir -p /tmp/fittizia && cp rehearsals/pizzeria-fittizia/{fixture,needs}.json /tmp/fittizia/
   PVAULT_PILOT_DIR=/tmp/fittizia go test ./internal/api/ -run TestPraticaPilotVault -count=1
   ```
   `server.json` there gives the address and the `scia` token; `touch /tmp/fittizia/done` stops it.
2. Claude opens the SUAP page in the browser pane and stops at the login. Liam logs in.
3. Claude follows the map screen by screen, filling from the vault and asking only for `ask:` items,
   and writes down every screen as it really is.
4. At each tap Claude stops: login is Liam's; pay and send never happen here; for signing, record
   what the portal wants.
5. Afterwards: update the map (what each screen really says, `seen:` dates), add a run below, delete
   the draft. A walk with nothing to fix moves the map to L1.

## Done means

- A walk goes from "I'm opening a pizzeria" to a saved draft with every box right and every
  attachment named, with Liam only logging in.
- The next walk, from scratch, needs no fix to the map.
- What we learned goes back where it belongs: the map to the library (with the `tap` field, open
  question 3), what the skill lacked to the pratica skill.

## Runs

| Date | Walked by | Reached | Broke on | Fixed |
|---|---|---|---|---|
| | | | | |
