# SCIA edilizia: works on structures, and every protection the asseverazione names

The SCIA for the CILA rehearsal's job: the architect with a procura designs and presents, the
tenant client, the owner company, two firms, and for works on structures Davide Esempio, the
structural engineer (`s:`). Made-up data (an invented 1965 licenza, PRG zone and article,
FAC-SIMILE documents in `files/`); each draft is "PROVA NON INVIARE" and stops before the
declarations, signing and sending.

`portal-steps.generated.yaml` is `gen.py`'s, from `recipe.yaml`, the crawl of an empty draft
(`catalogue/F839/sections/scia.json`: 36 sections, 319 choices) and `dictionary/fnsuap.yaml`.
The wizard's required Si/No (sanatoria) is answered No (`si_no` in the recipe).

## What it maps

- **What it files** (quadro c) and the asseverazione's tipologia): manutenzione straordinaria,
  restauro e risanamento conservativo or ristrutturazione edilizia (art. 22 c. 1 a) to c) DPR
  380/2001), each changing the use or not (one unit within its category, one unit in zone A, B
  or C to another category, a whole building within its category: art. 23-ter as the 2024 Salva
  Casa changed it); a variant in corso d'opera to a permesso di costruire or to a SCIA (essential
  or not), naming the title it changes; or a change of use alone, of a unit or a whole building,
  with no works or CILA works (art. 6-bis).
- **The contributo** (quadro g): free (the rule why), or due: paid (its receipt), before the
  works, in instalments or during the works, worked out on the architect's prospetto or by the
  SUE (the documents it needs). Each adds its slot.
- **With a landscape authorization**: the SCIA and the semplificata or the ordinaria in one
  pratica are their own recipes (`rehearsals/scia-paesaggistica-*-fittizia`), the asseverazione
  naming the procedure.
- **Structures.** The structural designer, another technician, in h1)'s block under the
  architect's (their albo, residence and studio); concrete or steel works and their art. 65
  denuncia; the seismic filing (Napoli is zone 2: L.R. Campania 9/1983, Reg. 4/2010): a
  deposito, an authorization, or a non-substantial variant to a project already filed (its
  protocol and date). Each filing's documents go in their own slot.
- **Protections**, one answer each, as the asseverazione's four quadri ask them: the
  landscape (none, or protected with works that need no authorization), the building as a
  bene culturale (the Soprintendenza), a protected natural area, the vincolo idrogeologico and
  idraulico, a VINCA, a cemetery's fascia, major-hazard plants (RIR), other ecological vincoli,
  and the fasce di rispetto (road, rail, power line, gas, military, airport, another).
- **A SCIA condizionata.** When any of those needs an assent, the request's documents go in
  the slot the answer adds, and section b) gets no start date: the works start once the SUE
  says the assents are in (the section's own text; the date isn't required).
- **The attachments screen** changes with the answers: each "si allega" adds a required slot,
  one per fascia for the tutela funzionale. All take plain PDFs (recipe `attachments`,
  `on_page`).

Refused at pre-flight (`supports`): a landscape authorization (semplificata or ordinaria: the
SCIA with it is its own recipe); works not allowed in a cemetery's fascia; acoustics, fire prevention, asbestos, a
hygiene derogation, the barriers rules, systems needing a project, energy filings. The
architect as structural designer too isn't mapped (her studio is the business's).

## Runs

| Date | Browser | Case | Result |
|---|---|---|---|
| 2026-10-28 | pane | P6 (plain) | several legs on one draft; 198 values and 7 attachments kept, every value read back on a fresh load |
| 2026-10-29 | pane | structures (deposito, art. 65, road fascia self-certified) | every section filled first time; stopped at Allega documenti on 3 required slots the answers add (read there, then mapped) |
| 2026-10-29 | pane | every protection needing an assent (restauro, seismic authorization) | every section filled first time, no start date accepted; stopped on 15 new required slots (read, mapped) |
| 2026-10-29 | pane | seismic variant, RIR area not mapped, self-certified vincoli (ristrutturazione) | every section filled first time; stopped on 4 new slots (read, mapped) |
| 2026-10-29 | pane | structures, from scratch | **filled** in 150 s with 10 attachments; fresh load 23 s, every value read back |
| 2026-10-29 | pane | every protection, from scratch | **filled** in 167 s with 22 attachments; fresh load 26 s, every value read back |
| 2026-10-29 | pane | seismic variant, from scratch | **filled** in 153 s with 11 attachments; fresh load 23 s, every value read back |
| 2026-10-29 | pane | P6 (plain), from scratch | **filled** in 136 s with 7 attachments; fresh load 23 s, every value read back |
| 2026-10-29 | pane | variant to a PdC (contributo paid, prospetto), then a change of use alone (unit, CILA works, between categories; contributo worked out by the SUE) | every section filled first time; each stopped at Allega documenti on the slots quadro g) adds (the prospetto, the receipt, the SUE's calculation documents), read and mapped |
| 2026-10-29 | pane | batch of all six cases from scratch, the three earlier ones now also changing the use (whole building, same category, between categories) | **5 of 6 filled** (P6 206 s, structures 166 s, every protection 218 s, seismic variant 168 s, change of use 155 s), each read back on a fresh load; the PdC variant stopped once on the firms' section, a box not yet on the page |
| 2026-10-29 | pane | PdC variant, rerun unchanged (after one more stop on the portal's "errore inatteso" page) | **filled** in 205 s, read back on a fresh load (31 s) |
| 2026-10-29 | pane | batch of the five cases not yet run, from scratch, with the runner that retries the portal's own errors: a variant that is a variazione essenziale; a change of use alone, of a unit with no works (within its category, between categories) or with art. 6-bis works (within its category), and of a whole building with 6-bis works | **4 of 5 filled** (essential variant 275 s, unit without works within its category 207 s, unit with 6-bis works 202 s, whole building 307 s), each read back on a fresh load, none needing a retry; the unit between categories stopped on Localizzazione, a street's suggestion list still fading out over "scala" |
| 2026-10-29 | pane | unit without works between categories, rerun with the runner that takes a fading list off the page | **filled** in 227 s, read back on a fresh load (62 s) |

What the 2026-10-28 stops taught (each fixed for every pratica):

1. The wizard reported "filled" while the portal refused its CONFERMA (the required Si/No):
   its screen check read the whole page, whose menu lists every screen. It now checks the
   title and answers Si/No questions (`answers`).
2. The licence number box takes 8 characters: the crawler now records each box's maximum
   and `run.py` refuses a longer value before the run.
3. The safety quadro's "e allega alla presente segnalazione la notifica" shows only with the
   notifica answer: `on_page` in the dictionary (by label, skipped where absent).
4. Surface and volume came back as "85.00": decimal boxes are compared as numbers.
5. After a resume the fresh-load check read a moved id: read-back now finds it by label too.
6. One regression of mine, caught by the run: a label-reader change let the box to the left's
   label count (the firms' block). Narrowed to skip only leading validation messages.

Still the person's: the "Documentazione dimostrativa dello stato legittimo" slot (signed
files only, `.p7m`), the procura and the "Quadro riepilogativo - SCIA DSC" (portal
documents), naming the direttore dei lavori (and the structures' one), the declarations,
signing and sending.
