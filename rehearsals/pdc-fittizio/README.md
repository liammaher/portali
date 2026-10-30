# Permesso di costruire (art. 10 DPR 380/2001)

The permesso for the CILA rehearsal's job: the architect with a procura designs and presents, for
the tenant client, with the owner company's assent and two firms. The P6 case: a ristrutturazione
edilizia of art. 10 c. 1 c (demolished and rebuilt, more volume, three storeys in reinforced
concrete), the contributo worked out by the SUE and paid when the permesso is collected, under
the energy rules (with an art. 14 c. 6 incentive), the renewables, the passive acoustics and the
barriers rules. Made-up data (rehearsals/cila-fittizia's people and property); the draft is
"PROVA NON INVIARE" and stops before the declarations, payment, signing and sending.

`portal-steps.generated.yaml` is `gen.py`'s, from `recipe.yaml`, the crawl of an empty draft
(`catalogue/F839/sections/sue-permesso-di-costruire-art-10-d-p-r-380-2001.json`: 37 sections)
and `dictionary/fnsuap.yaml`. The wizard's Si/No (sanatoria) is answered No.

## What it maps

- **What it asks for** (quadro a) and the asseverazione's tipologia): a new building, a
  ristrutturazione urbanistica or an art. 10 c. 1 c ristrutturazione edilizia (each changing the
  use or not, as the SCIA's four cases), works a SCIA could file asked as a permesso (art. 22
  c. 7), or an essential variant to a permesso (its number and date).
- **The contributo** (quadro f): free (the rule why), or due: worked out on the architect's
  prospetto or by the SUE, paid when the permesso is collected, in instalments or during the
  works. No opere a scomputo (its Si/No is No).
- **g1)**: the architect presents it ("colui che presenta la pratica": studio, albo, contacts);
  the structural designer and the direttori dei lavori are named before the works start.
- **The asseverazione's own quadri**: surfaces (s.l.p. and superficie coperta), volume, storeys,
  the town plan; the barriers rules (the requirements met, their drawings attached); systems;
  the energy report and the renewables (both filed with the comunicazione di inizio lavori) and
  the incentive the portal then requires; acoustics (art. 8 L. 447/1995 and the DPCM 5/12/1997
  asked apart); excavated earth; fire prevention without the Vigili del Fuoco's assessment;
  asbestos (its plan sent 30 days before the works); structures (the art. 65 denuncia and the
  seismic deposito with the inizio lavori, or the seismic authorization asked now); soil,
  urbanisation and drains (none, or the sewer connection got or asked); every protection the
  SCIA maps (no box to attach for a bene culturale: the SUE asks the Soprintendenza); the
  Autorità di Bacino's plans.
- **The attachments screen** changes with the answers: the co-holders' assent and IDs, the
  barriers drawings. The stato legittimo documents, the procura, the DSC, the receipt and the
  broadband infrastructure drawing (art. 135-bis) take only signed files or are portal
  documents: the architect's. The permesso has no slot for the contributo's documents: the
  architect attaches them, signed, under Allegati liberi.

Refused at pre-flight (`supports`): a landscape authorization (its own operation, filed with the
permesso: not mapped yet); works not allowed in a cemetery's fascia; an acoustic impact or
clima acustico report; a project for the Vigili del Fuoco; a hygiene derogation; systems
needing a project; soil needing investigations; an area without its urbanisation; with the energy
rules or the renewables, no incentive (the portal's form can't say none); a contributo already
paid before filing. Not offered: the sanatoria (art. 36), a deroga (art. 14), the permesso
convenzionato, the piano casa, the titolo unico, a SUAP procedure.

## Runs

| Date | Browser | Case | Result |
|---|---|---|---|
| 2026-10-29 | pane | P6 (plain), first legs on one draft | sections 0-14 filled first time (a), f), g1) included); then stopped six times, each fixed for every pratica and resumed on the same draft: the firm's opening radio worded "saranno eseguiti" (a fourth `if_present` variant), the safety notifica sent before the works (an `on_page` radio), the asseverazione's two opening radios kept as one by the portal, the energy rules requiring an incentive claimed, then two on the attachments screen (the slots the answers add, read and recorded in the crawl; the procura's box named "Scarica il documento") |
| 2026-10-29 | pane | P6 (plain), from scratch, in a batch with the SCIA alternativa | **filled** in 213 s with 7 attachments; fresh load 33 s, every value read back; no retry |
| 2026-10-29 | pane | P6, from scratch after the shared acoustic/fire and ecological quadri changed | **filled** in 141 s; fresh load 23 s, every value read back; no retry |
