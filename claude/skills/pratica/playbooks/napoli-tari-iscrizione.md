# Comune di Napoli — TARI nuova iscrizione (uso abitativo)

Last verified: 2026-10-26
Library: napoli/comune/tari-iscrizione-uso-abitativo
Applies to: whoever starts occupying or holding a home in Napoli, once, within 90 days of
the start. The library entry has the procedure itself: sources, deadline and lateness
sanction, channels, payment (F24 codes, 2026 schedule, simulator), documents, the Mod. 1
field mapping, steps and pitfalls. Read it first; this file keeps only what it lacks.

## Sources

See the library entry. Nothing extra used here.

## Deadline

In the library (90 days, verified on the 2023 regolamento). When the user is past it, say
so and let the Comune compute the sanction; don't estimate it.

## Channels

In the library. PEC is the preferred one when `identity.pec` is set: it gives a legal
receipt, which is what the vault should archive.

## Documents and form

In the library. The identity copy can come from the vault's `cie` or `passaporto`
documents (`pvault doc list cie`).

## Vault fields

The library's `forms[mod-1].fields` is the mapping. Its conviventi row (`repeat:
nucleo_familiare.membro<N>_*` until the library is updated) means the family members
whose `<slug>:famiglia.stato_famiglia` is si: `<slug>:identity.{last_name,first_name,
birth_comune,date_of_birth,codice_fiscale}`, rapporto = `relazione_label` from
`vault_household` / `pvault person list`. Tokens for this pratica, if Touch ID is off
(the skill's step 2):

- preparing: `--scope "*:identity.*,*:famiglia.*,residenza.*,domicilio.*,abitazione.*,tari.*,pratiche.*" --access append`
- tracking: `--scope "pratiche.*,tari.*" --access write`

Leave `locazione.*` out: the form doesn't need the rent or the landlord.

## Steps

The library's. Before filling, check the user's TARI folder and `pvault list pratiche`
for a Mod. 1 already filled or signed, and don't regenerate a signed one.

## Needs the user

Signature, sending the PEC/email (or the raccomandata), paying the F24.

## After submission

- Save `pratiche.<slug>_data_invio`, `_stato` = `inviata`; archive the PEC receipt with
  `pvault doc add tari <file>`.
- The Comune replies (or a payment notice arrives): save the library entry's `records`
  (codice contribuente, date and protocollo; taxed area and occupants from the notice), and
  check the notice matches `abitazione.superficie_calpestabile_mq` and the household
  (1 + the members whose `famiglia.stato_famiglia` is si).
- Close the pratica: `_stato` = `completata`, `_data_esito`.
- The F24 is tracked as its own pratica: `importo`, `data_scadenza`, then
  `data_pagamento` when paid.

## Gotchas

- The library's form mapping uses `identity.birth_country` for "nato/a a (stato estero)";
  leave it empty for someone born in Italy (the vault refuses Italia there).

## Run log

- 2026-10-25: first run. Found a signed Mod. 1 that had never been sent and an unpaid F24
  for the 2026 unica soluzione, whose deadline had already passed; tracked them as two
  pratiche (iscrizione and acconto), the iscrizione on hold at the user's request.
- 2026-10-26: trimmed to what the library lacks. The library has since verified the
  90-day deadline (regolamento 2023), confirmed the current Mod. 1 prints the new simulator
  link, and lists the POTESs portal as a channel.
