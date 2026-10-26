# Comune di Napoli — TARI nuova iscrizione (uso abitativo)

Last verified: 2026-10-25
Library: napoli/comune/tari-iscrizione-uso-abitativo
Applies to: whoever starts occupying or holding a home in Napoli (owner, tenant or
other), once, when the occupation starts. The declaration stays valid for later
years until something changes (then Mod. variazione / cessazione).

## Sources

- Form: **Mod. 1 TARI – Nuova iscrizione uso abitativo**, from the Comune's TARI pages.
  Its AVVERTENZE and INFORMAZIONI boxes list the channels and contacts (checked 2026-10-25).
- [TARI 2026 — Comune di Napoli](https://www.comune.napoli.it/articolo_tematico/tributi-locali/tari-tassa-sui-rifiuti/tari-2026/)
  (page updated 2026-10-02): payment deadlines and the F24 simulator.
- Regolamento TARI: delibera C.C. n. 16 del 29.9.2020 is the latest found; the article on
  the declaration deadline was not checked.

## Deadline

- **Declaration**: not confirmed for Napoli. Many comuni require it within 90 days of
  the start of occupation; others accept it until 30 June of the following year. Check
  the current regolamento before telling the user it's late, and file as soon as possible.
- **Payment 2026** (autoliquidazione: due even without a payment notice). Acconto = 75%
  of the amount at 2025 tariffs:
  - unica soluzione 15/10/2026, or rate 30/09, 30/10 and 30/11/2026
  - saldo at 2026 tariffs 16/02/2027 (the Comune sends a new notice)
- Late payment: ravvedimento operoso is possible; let the Comune's simulator or the
  sportello compute the sanzione and interest, don't estimate it.

## Channels

Any one of:

- email to `entrate.tari-tares-tarsu@comune.napoli.it`
- PEC to `entrate.tari-tares-tarsu@pec.comune.napoli.it` (gives a legal receipt; preferred
  when the user has a PEC)
- raccomandata to Comune di Napoli, Area Entrate, Servizio IMU e TARI, Via Diocleziano 330,
  80125 Napoli
- sportello at the same address, Mon–Fri 8:30–12:30 (appointment via the Comune's online
  booking service)

Phone: 081 795 3711, every day 9:00–13:00.

## Documents and form

1. Mod. 1 filled in and **signed** (firma is a required field).
2. Copy of an identity document (art. 38 DPR 445/2000): CIE or passport, from the vault's
   `cie` or `passaporto` documents or the user's folder.
3. Optional: visura catastale if the user doesn't know the catasto data.

## Vault fields

| Form field | Field ID |
|---|---|
| Il/La sottoscritto/a | `identity.last_name` + `identity.first_name` |
| nato/a a, il | `identity.birth_comune` (+ country if abroad), `identity.date_of_birth` |
| C.F. | `identity.codice_fiscale` |
| residente in, Via, n° | `residenza.comune` (`residenza.provincia`), `residenza.indirizzo`, `residenza.civico` |
| Telefono, e-mail, pec | `identity.phone`, `identity.email`, `identity.pec` |
| a decorrere dal | `abitazione.data_inizio` |
| qualità di | `abitazione.titolo` (proprietario / possessore / locatario / altro) |
| Appartamento sito in via | `domicilio.*` if set, else `residenza.*`, with `abitazione.piano` and `*.interno` |
| Sez., Foglio, Part., Sub. | `abitazione.catasto_sezione`, `_foglio`, `_particella`, `_subalterno` |
| Superficie calpestabile in mq | `abitazione.superficie_calpestabile_mq` |
| Conviventi (nome, luogo e data di nascita, rapporto, C.F.) | `nucleo_familiare.membro<N>_*` |
| Cod.Contr. | leave blank on a new iscrizione; the Comune assigns it (`tari.codice_contribuente`) |

Tokens for this pratica (see the skill's step 2):

- preparing: `--scope "identity.*,residenza.*,domicilio.*,abitazione.*,nucleo_familiare.*,tari.*,pratiche.*" --access append`
- tracking: `--scope "pratiche.*,tari.*" --access write`

Leave `locazione.*` out: the form doesn't need the rent or the landlord.

## Steps

1. Ask whether a Mod. 1 was already filled, signed or sent; look in the user's TARI folder.
2. Fill the PDF from the vault (pdf skill); the user signs it.
3. Check the payment position: compute the F24 with the simulator (see Gotchas) or use a
   payment notice. Codice ente F839; codici tributo 3944 (TARI) and TEFA; rateazione 0101
   is the unica soluzione.
4. Hand off: signed form + ID copy, sent by email or PEC from the address written on the form.
5. Track the reply and record the codice contribuente.

## Needs the user

Signature, sending the email/PEC (or the raccomandata), paying the F24.

## After submission

- Save `pratiche.<slug>_data_invio`, `_stato` = `inviata`; keep the sent email or PEC receipt.
- The Comune replies (or a payment notice arrives) with the Cod.Contr.: save
  `tari.codice_contribuente`, `tari.data_dichiarazione`, `tari.protocollo`; from the notice
  also `tari.superficie_tassata_mq` and `tari.componenti_tassati`, and check they match
  `abitazione.superficie_calpestabile_mq` and the household.
- Close the pratica: `_stato` = `completata`, `_data_esito`.

## Gotchas

- The form prints the old simulator link (`riscotel.it/calcolotari/napoli`). The Comune's
  TARI 2026 page points to `https://portaletributi.comune.napoli.it/?ente=NAPO`; use that.
- Payment is in autoliquidazione: a new occupant who never received a notice still owes the
  2026 rate.
- A convivente without an Italian codice fiscale was entered as `N/E`; the Comune hasn't
  confirmed that's accepted.
- The "Uso stagionale" 40% reduction is only for a second home when the user already has a
  TARI position for their main home; leave it blank otherwise.

## Run log

- 2026-10-25: first run. Found a signed Mod. 1 that had never been sent and an unpaid F24
  for the 2026 unica soluzione, whose deadline had already passed; tracked them as two
  pratiche (iscrizione and acconto), the iscrizione on hold at the user's request.
  Found the moved simulator URL. Declaration deadline still unverified.
