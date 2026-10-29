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

- **The works** (art. 22 c. 1 a) to c) DPR 380/2001): manutenzione straordinaria, restauro e
  risanamento conservativo, ristrutturazione edilizia, without a change of use (quadro c) and
  the asseverazione's tipologia).
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

Refused at pre-flight (`supports`): a change of use; a landscape authorization (semplificata
or ordinaria: on Napoli's SUE it's its own pratica, selected with the SCIA, a combination not
mapped yet); works not allowed in a cemetery's fascia; acoustics, fire prevention, asbestos, a
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
