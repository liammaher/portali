# The official CILA schema against the SUE portal

Compared on 2026-10-28: InfoCamere's `mod_cila_v1.0.7.xsd` (repository
`InfoCamereSCpA/Sistema-Informatico-degli-Sportelli-Unici-Moduli-PA`, commit `fc13e08`, 12 March
2026), flattened by `flatten.py` into `mod_cila_v1.0.7-fields.json`, against every field seen on
the SUE of Napoli in the first walk (`../portal-map.yaml`), up to quadro h). Matching is by
meaning, done by hand: "same" means the schema has a field asking the same thing with the same
choices, wherever it sits in the schema.

## Result

Of the **152 portal fields** seen in the 17 walked sections:

| | Fields | Share |
|---|---|---|
| Same field in the schema | 133 | 87.5% |
| In the schema, but asked elsewhere | 4 | 2.6% |
| Only on the portal | 15 | 9.9% |

So about **9 in 10 of the portal's questions, and every branch walked, come straight from the
official schema**. What the schema can't give: the portal's field ids, widgets and option lists,
the exact wording of each question, and the 15 portal-only fields below.

## Section by section

| Portal section | Fields | Same | Elsewhere | Portal only | Schema path |
|---|---|---|---|---|---|
| Scheda anagrafica (persona) | 22 | 19 | 2 | 1 | `scheda_anagrafica/schedaTitolare` |
| Qualifica e dati del procuratore | 19 | 17 | | 2 | `scheda_anagrafica/schedaDelegato` |
| Oggetto della pratica | 1 | | | 1 | (none) |
| Domicilio digitale | 2 | | | 2 | (none) |
| Domicilio elettronico | 2 | | 1 | 1 | `schedaDitta/domicilioElettronicoComunicazioni` |
| Localizzazione dell'intervento | 9 | 8 | | 1 | `comunicazioni/localizzazione_intervento/immobile/ubicazioneImmobile` |
| Dati catastali | 8 | 5 | | 3 | `…/immobile/riferimentiCatastali/datiCatastali` |
| a) Titolarità | 4 | 4 | | | `dichiarazioni/titolo_possesso` |
| a) the terzi titolari table | 21 | 20 | | 1 | `allegatoSoggettiCoinvolti/titolari` |
| b) Parti comuni | 4 | 4 | | | `dichiarazioni/opere_previste` (the same four) |
| c) Presentazione della CILA | 1 | | | 1 | (the schema's c.1–c.3, see below) |
| d) Qualificazione | 5 | 4 | 1 | | `comunicazioni/qualificazione_intervento` |
| e) Destinazione d'uso | 1 | 1 | | | `…/datiCatastali/destinazione_uso` |
| f) Contributo di costruzione | 4 | 4 | | | `comunicazioni/calcolo_contributo_costruzione` (the same four) |
| G1) Tecnici incaricati | 16 | 14 | | 2 | `allegatoSoggettiCoinvolti/tecnici_incaricati/schedaDirettoreOpereArchitettoniche` (studio, ordine) |
| G2) Altri tecnici | 2 | 2 | | | `comunicazioni/tecnici_incaricati` |
| h) Imprese esecutrici, with one firm | 31 | 31 | | | `allegatoSoggettiCoinvolti/imprese_esecutrici`, DURC included (Cassa edile, INPS, INAIL) |
| **Total** | **152** | **133** | **4** | **15** | |

The declarations (consapevolezza, privacy, tasse comunali, art. 75) weren't opened, being the
person's, and aren't counted.

## Only on the portal (15)

- **Sesso**, three times (titolare, procuratore, terzo titolare): the schema leaves it out, since
  the codice fiscale holds it.
- **Qualifica del procuratore** (professionista incaricato, notaio, …).
- **Oggetto della pratica**: the portal's own one-line description.
- **Domicilio digitale** INAD / INI-PEC (two boxes): newer than the schema (CAD art. 3-bis).
- The **PEC consapevolezza** box under the domicilio elettronico.
- **Frazione** in the address.
- **Catasto**: sezione amministrativa, classe, categoria (the schema has one `sezione`).
- **c) PER PRESA VISIONE**: the portal's acknowledgement.
- **G1 "colui che presenta la pratica / non è"** (two radios): the portal reuses the
  procuratore's data instead of asking the progettista's name again.

## Elsewhere (4)

- "In qualità di" + "specificare" in the Scheda anagrafica: the schema keeps the title once, in
  `titolo_possesso/titolo` (the portal asks it twice).
- The domicilio elettronico's PEC: in the schema only for a company (`schedaDitta`).
- d.2's 333 € receipt: the portal has an upload box inside quadro d; the schema has it among the
  attachments (`allegatoRicevutaVersamentoSanzione`).

## In the schema, not seen on the portal

- **c.1 / c.2 / c.3** (`no_altri_atti`, `comunicazioni_certificate`, `domanda_contestuale_cila`):
  the portal takes these from the procedures picked in the wizard.
- `procedimento_automatizzato` / `procedimento_ordinario` in d), `ufficio_provinciale_catastale`,
  `coordinate`: none showed on the walked screens.
- Two choices the schema has as free text or booleans are fixed lists on the portal: the titolo
  (proprietario … altro) and the destinazione d'uso (residenziale, industriale, commerciale,
  altro). The option lists have to come from the portal.

## What the schema already says about the sections not walked yet

- **i) Salute e sicurezza** (`comunicazioni/obblighi_salute_sicurezza`): non ricade; ricade, sotto
  200 uomini-giorno or 200 and over; senza notifica; con notifica preliminare allegata; or "si
  riserva di presentarla prima dell'inizio lavori".
- **Relazione tecnica** (`dichiarazioni_progettista`): tipo di intervento (manutenzione
  straordinaria, restauro e risanamento, barriere architettoniche, sottosuolo, movimenti terra,
  serre, pertinenze minori, altro + specificare) and a free-text detail; tolleranze costruttive
  (a long list by surface and seismic zone); altre comunicazioni; atti di assenso (tipo, autorità);
  note.
- **Attachments**: the SUE list the CILA uses has 36 kinds, procura, soggetti coinvolti, diritti,
  identity document, assenso dei terzi, sanzione receipt and notifica preliminare among them.

These are predictions to check, not walked fields.

## What this means

- The schema is a sound source for **the questions and their branches**: it got every choice the
  walk met (b, d, f, G2, h), and named the soggetti coinvolti and DURC fields before the portal
  showed them.
- The portal adds a thin layer of its own (about 1 field in 10), plus everything about *how* to
  fill: ids, dropdown values, lookups, ids that shift when a choice adds a row, warnings.
- So the plan holds: questions from the schema, a crawler for the portal layer, the runner to
  fill. The portal layer still needs one look per section.
- Limits of this check: one form, the walked 17 of 22 sections, matched by one person (Claude,
  who also walked the portal), and Napoli's own Mod.CILA may differ from the national schema in
  sections not reached.
