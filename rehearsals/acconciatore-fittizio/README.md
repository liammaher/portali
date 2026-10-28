# The first SUAP pratica: a hairdresser's opening, drafted from the national schema

"Aprire l'attività di acconciatore" on Napoli's SUAP (impresainungiorno), for a made-up ditta
individuale, Salone Fittizio di Anna Fittizia (codice fiscale and partita IVA pass the
checksums and belong to nobody), opening at Via Alessandro Scarlatti 100. The business itself
declares. The draft is "PROVA NON INVIARE" and stops before the declarations, signing and
sending.

How its steps file was made (`portal-steps.generated.yaml`, by `gen.py`):

- **Crawl**: an empty draft, 91 s (`catalogue/F839/sections/acconciatore.json`). 9 sections
  to fill; the rest are the person's declarations.
- **Shared sections** (Scheda anagrafica, domicili, the premises, catasto, oggetto, diritti):
  the dictionary's, in their business version (`subject: impresa`), written from the
  pizzeria walk (`rehearsals/pizzeria-fittizia`).
- **The three sections of its own** ("Dati sull'attività", the antimafia declaration, the
  professional qualifications): drafted by `draft.py` from the national form
  `mod_acconciatori_estetisti_apertura` (`claude/skills/pratica/portal/schema/`) and put in
  the dictionary as drafted, no hand edits. 31 boxes: each got its schema meaning (surfaces,
  shopping centre, sole or joint activity, the technical manager's qualification and its
  issuer, date and number); the antimafia statement came out as the person's (`theirs`);
  the Allegato B upload as a file question.

The drafter was improved on this same pratica until its draft was right (box order, Si/No
pairs, repeated labels, what a choice opens), so "no hand edits" is measured on the pratica
it was tuned on. A second SUAP pratica is the fair test. The questions read like the portal's
labels ("di essere Responsabile tecnico?"); wording them for a person is still to do.

## Runs

| Date | Browser | Login → draft | Result |
|---|---|---|---|
| 2026-10-28 | pane | 50.8 s (wizard 9.8, 10 sections, fresh load 7.3), then the attachments screen | First run, no stop: 53 values, every one read back on a fresh load |

Every attachments slot (planimetria quotata, relazione descrittiva, the fee receipt) takes
only signed files (`.p7m`): the person's. `gen.py` now leaves every signed-only slot to the
person.
