# Vault sensitivity ranking: what goes in 1Password, what stays in the vault

Every field the vault's schema (`schema.json`) can hold, ranked into three tiers. Written 2026-10-27 as
part of the strategy discussion in this folder; see `MVP checklist.md` for where this fits.

## The three tiers

| Tier | Where it lives | Rule for the agent | What belongs here |
|---|---|---|---|
| **1. Secret** | **1Password** | Never reads it. 1Password fills it in on his approval | Anything that lets someone **act as him or move his money** |
| **2. Sensitive** | Vault, encrypted | Reads it only for a specific task, with his approval, and every read is logged | Anything that **enables identity fraud or exposes his private life** |
| **3. Standard** | Vault | Reads it freely for any task | Public facts, or facts with little harm if leaked |

The vault already has five tiers, and they map onto these three: `personal` → Secret, `sensitive` and
`critical` → Sensitive, `standard` and `public` → Standard.

## Field by field

| Category | Secret (1Password) | Sensitive (vault, ask first) | Standard (vault) |
|---|---|---|---|
| `spid` | username, password | — | provider, level, renewal date *(change)* |
| `cie` | PIN, PUK | number | issue and expiry dates, issuing comune |
| PEC and portal logins *(not in the schema; in the Keychain today)* | PEC password and app password, portal logins | — | — |
| `payment` | card number, expiry, cardholder name | — | card brand |
| `identity` | — | codice fiscale, date of birth, personal phone, **citizenship** *(change)* | names, display name, email, PEC address, sex, birth comune, birth province, birth country |
| `residenza`, `domicilio` | — | street and civico | flat number, CAP, comune, province, state, start date, computed quartiere, municipalità, ASL district |
| `abitazione` | — | — | title, catasto data, rendita, m², rooms, floor, energy class |
| `locazione` | — | rent, condominium fees, deposit; the landlord's name, codice fiscale, IBAN, email, PEC, phone | contract type, dates, notice period, cedolare secca, registration details, building administrator |
| `utenze` | — | customer codes | POD, PDR, suppliers, account holders |
| `tari` | — | — | contributor code, declaration date, protocollo, taxed m², occupants |
| `famiglia` | — | dependant percentage; anything about minors | relationships, stato di famiglia, parents, legal representation |
| `ruolo`, `procura` | — | — | role, dates, share, powers; procura body, subject, dates |
| `business` | — | — | all of it: partita IVA, name, ATECO, REA, PEC, sede, INPS and INAIL numbers, CCNL, DURC and PEC expiry, TARI code |
| `locale` | — | — | all of it: address, catasto data, m², use, TARI data, computed municipalità |
| `albo`, `cassa`, `firma_digitale` | — | — | order, number, dates, pension fund ID, signature issuer and expiry |
| `permesso_soggiorno` | — | number, **type** *(change)* | issue and expiry dates, issuing questura |
| `passaporto`, `patente`, `tessera_sanitaria` | — | document numbers | country, authority, dates, licence categories, region, GP |
| `financial` | — | IBAN | — *(the US-only `ssn` and `filing_status` fields: drop them)* |
| `medical` (empty today) | — | **everything**, as special-category data | — |
| `pratiche` | — | ones about health, legal status or minors | the rest |
| documents (scans) | — | ID scans, medical and legal documents | business documents, receipts, forms |
| `preferences`, `employment`, legacy `addresses` | — | `addresses` street *(a duplicate of `residenza`: drop it)* | timezone, language, how he logs into the PA, employer, job title |

## Why each tier

- **Secret:** these let someone **become him** (a SPID login, a CIE PIN) or **take his money** (cards).
  PEC passwords belong here too: CERT-AgID counted PEC attacks up about 80% in 2025, and a compromised
  PEC box can send fraudulent PECs that look legitimate.
- **Sensitive:**
  - **Codice fiscale, document numbers, date of birth, home address:** the raw material of identity
    fraud. CRIF counted more than 38,300 cases of credit fraud through stolen identity in Italy in 2025
    (up 23.8%), and Campania was third by number of cases. In 2017 the Garante warned that a codice
    fiscale alone was enough to see someone's tax data through an Agenzia delle Entrate web service.
  - **IBAN:** someone else's IBAN is enough to set up a SEPA direct debit on their account. The Banque de
    France puts that fraud at over 60% of direct-debit fraud.
  - **Health, and legal or criminal data:** special categories under GDPR art. 9 and art. 10. For Accoglienza Fittizia
    they need the strictest handling.
  - **Citizenship and permit type:** not special categories by law, but they can reveal ethnic origin or
    asylum status. Treat them as sensitive.
- **Standard:**
  - **Business details:** already public in the Registro Imprese, and INI-PEC lists the business PEC.
  - **The locale:** a shop front is public.
  - **Catasto data:** anyone can obtain it with a visura.
  - **Expiry dates and computed areas:** they reveal little on their own.

## Recommended changes to the vault's current tiers

1. **`spid.provider`, `livello`, `data_scadenza`:** from `personal` to standard. The agent needs to know
   "log in with PosteID" and to track the renewal. Only the username and password are secret.
2. **`identity.citizenship`:** from standard to sensitive.
3. **`permesso_soggiorno.tipo`:** from standard to sensitive (it can reveal asylum or protection status).
4. **`payment.*`:** move to 1Password (it's `critical` in the vault today).
5. **Credentials** (SPID login, CIE PIN and PUK, PEC password): move to 1Password. The vault's
   `personal` tier can simply stop holding them.
6. **`financial.ssn`, `financial.filing_status` and `addresses.*`:** drop them. They're leftovers from the
   US original.
7. **`medical`:** if it's ever used, make it sensitive with a special-category flag.

None of these changes is made in the code yet.

## Sources

- [Teleborsa: CRIF fraud observatory, Aug 2026](https://www.teleborsa.it/DettaglioNews/107_2026-08-03_TLB/Osservatorio-CRIF-frodi-creditizie-in-crescita.html)
- [La Sicilia: fraud by region](https://www.lasicilia.it/news/cronaca/3065756/frodi-creditizie-la-sicilia-e-seconda-in-italia-i-truffatori-made-in-italy-arrivano-soprattutto-da-lombardia-e-isola.html)
- [Il Giorno: identity fraud](https://www.ilgiorno.it/economia/furto-identita-truffe-1cb650db)
- [money.it: identity theft](https://www.money.it/furto-identita-cosa-fare-e-come-denunciare)
- [OrizzonteScuola: the Garante on tax data, 2017](https://www.orizzontescuola.it/sicurezza-dati-garante-della-privacy-scrive-gentiloni-la-diffusione-sui-dati-fiscali/)
- [Garante: stolen ID cards, 1999](https://garanteprivacy.it/garante/doc.jsp?ID=47747)
- [Brocardi: GDPR art. 9](https://brocardi.it/regolamento-privacy-ue/capo-ii/art9.html)
- [Legiscope: special categories](https://www.legiscope.com/blog/categorie-particolari-dati-articolo-9.html)
- [Finance Magnates: IBAN fraud in SEPA direct debits](https://www.financemagnates.com/fintech/bnp-paribas-taps-worldline-to-fight-iban-fraud-in-sepa-direct-debits/)
- [Worldline: direct-debit fraud](https://worldline.com/en/home/main-navigation/resources/blogs/2024/how-businesses-can-combat-direct-debit-fraud)
- [CERT-AgID Report 2025](https://www.agid.gov.it/sites/agid/files/2026-02/Report%20CERT-AGID%202025.pdf)
- [Agenda Digitale: PagoPA phishing](https://www.agendadigitale.eu/cittadinanza-digitale/phishing-a-tema-pagopa-come-funziona-la-trappola-e-perche-rende/)
- [TuttoAndroid: fake PEC fines](https://www.tuttoandroid.net/news/2026/02/23/truffa-false-multe-pec-pagopa-come-riconoscerle-1141005/)
- [1Password: item categories](https://support.1password.com/item-categories)
