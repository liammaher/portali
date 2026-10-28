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

Next in the chain: TARI non domestica by PEC (below), then the insegna and the dehors.

## The second pratica: TARI non domestica by PEC

The Mod. 5 (library: `napoli/comune/tari-iscrizione-non-domestica`), the declaration a business
makes within 90 days of occupying its premises; here from 1 November 2026. PEC is its preferred
route and takes a handwritten signature scanned with an ID copy, so no firma digitale is needed.

- **Filled** by `fill.py` from this business vault (the regression pilot `mod-5`): 12/12 checks,
  every value back in its box, titolare ticked, the second premises row empty. The library got
  the form's layout (pratiche-napoli, `layouts/.../tari-iscrizione-non-domestica/mod-5.json`) and
  a mapping that matches the form (born abroad in one line, the tick boxes, what is optional).
- **Not signed through the vault:** the sign gate signs only for a personal vault's owner, and a
  business's titolare is a person in a business vault (pilot case `mod-5-sign`). So the titolare
  prints, signs by hand and scans, or the gate learns to sign for a business's people.
- **To send**, the person's: from the ditta's PEC to entrate.tari-tares-tarsu@pec.comune.napoli.it,
  with the signed Mod. 5, a copy of the titolare's ID and the planimetria (measures and use of each
  area); keep the PEC receipts. Then the F24 from the Comune's simulator. Never sent here.

## Lines we don't cross

- **Never send.** An invented declaration sent to an office is a false statement (DPR 445/2000,
  art. 76). A draft isn't a declaration.
- **Never pay, and don't create a pagoPA position**: stop before "Procedi con il pagamento tramite
  pagoPA".
- **Never tick a declaration** in the person's name; they do.
- **Keep or delete the draft** as the person says (Pratiche in bozza); never send it.
- **Logins are the person's.** Liam logs in with his own SPID; Claude never types credentials.
- **No real third party's data.** The titolare is Gennaro Fittizio. If the portal ties the titolare
  to whoever is logged in, record it, and use Liam's own identity for that walk.

## Doing it in one command

```sh
rehearsals/pizzeria-fittizia/do.py scia     # the SCIA on the SUAP portal, to a saved draft
PRATICHE_NAPOLI_DIR=<library> rehearsals/pizzeria-fittizia/do.py tari   # the TARI Mod. 5, filled
```

Each serves the made-up vault, reads what the pratica needs and stops the vault. `scia`
opens a Playwright window (log in there with SPID), runs `run.py run` and leaves the draft
open at the person's steps. `tari` fills the Mod. 5 with `fill.py`, reads all 70 values back,
opens it for review and prints the route: sign it, then send it with the ID copy and the
planimetria from the business's PEC. Output: `~/.cache/pratica-rehearsal/pizzeria-fittizia/`.

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
| 2026-10-27 | Claude, Liam logged in | Allega documenti: 21 screens, every factual section saved | The attachments gate: two required files must be CAdES-signed (.p7m); the seven declarations are the person's | Map rewritten from what the portal showed |
| 2026-10-27 | Claude from `portal-steps.yaml`, in the Claude browser (already logged in) | Every factual section saved and read back; stopped before the declarations and payment | Nothing in the map; two mechanics: a dropdown filter matches substrings, and a click on a fresh panel misses | The filter-and-arrow recipe, in the map and in `run.py` |
| 2026-10-27 | Benchmark: 3 timed runs from the steps file, bigger batches | Every factual section saved and read back, each run | Run 1: a re-render after Forma giuridica wiped CF and P.IVA; run 3: it wiped Denominazione | Forma giuridica now comes before Denominazione in `portal-steps.yaml` |
| 2026-10-27 | `inpage.js` in the Claude browser, one call to start it | Every factual section saved and read back, in 53 s | Four things the first tries found: the pane slows page timers, dropdowns build their list on first open, the last wizard screen loads twice, labels sit above or left of their box | All four in `inpage.js` and the shared label check |
| 2026-10-28 | `do.py scia`: `run.py run` in a Playwright window, Liam logged in there | Every factual section saved and read back, 45 s after the login | Nothing | |
| 2026-10-28 | `run.py run --vault-dir ~/.pvault-fittizia`: the real business vault, as the pratica skill does it | Every factual section saved and read back, 58 s | A window left open by the last run held the profile ("Opening in existing browser session") | Ran with `--profile` on a new folder |

Time: about an hour for the first walk, **14 min 51 s** for the second (7 wizard screens in
about 4 minutes, the 9 sections in about 11), with the steps known and every value computed
before starting. The read-back caught the one wrong value (CASALNUOVO DI NAPOLI) before
saving. `claude/skills/pratica/portal/run.py` runs the same steps without a model in a
separate Chrome; its live run is still to do, in a window the person logs into.

**Benchmark** (2026-10-27): the same draft five times, from the dashboard to the saved diritti
section, each run started on a fresh draft with Liam already logged in. Runs 1-3 in bigger
batches (up to 25 browser actions each, about 13 per run) instead of one call per field.

| Run | Time | Notes |
|---|---|---|
| 1 | 6 min 59 s | One recovery: CF and P.IVA retyped after the Forma giuridica re-render |
| 2 | 6 min 30 s | No retries |
| 3 | 7 min 23 s | Denominazione retyped after the same re-render; a context reset mid-run included |
| 4 | **1 min 7 s** | `inpage.js`: 53 s in the page (wizard 11 s, anagrafica 20 s, each other section 2-4 s), the rest reading the vault and starting it |
| 5 | **45 s** | `do.py scia` (2026-10-28): the same runner in a Playwright window, from the login to the saved draft (wizard 11 s, anagrafica 17 s); nothing slows its timers there |

Runs 1-3 spent half their time on the model between batches and most of the rest on fixed
pauses. Run 4 takes the model out of the loop: `run.py page` writes the values as one small
script, and `inpage.js` (installed once) drives the portal in a same-origin frame over the page,
so a save's reload doesn't end it. It sets dropdowns through their PrimeFaces widgets, waits on
the portal's ajax queue instead of a clock, and reads every field back after each save. So a
documented portal pratica takes about **a minute**, against about an hour the first time; what
is left is the portal's own page loads.

What the first walk taught, beyond the map:

- **Declarations are a tap of their own.** Seven sections are the dichiarante's statements
  (DPR 445/2000, requisiti, sorvegliabilità…). Claude fills facts; the person ticks those, so
  `declare` joins login, sign, pay and send.
- **Portal attachments need a firma digitale in CAdES (.p7m)**, so the sign gate's stamped
  signature can't serve here (north star, open question 2).
- **The portal checks nothing against the login or the registers in a draft**: a made-up
  titolare, CF and P.IVA saved without complaint. Correctness is ours to guarantee.
- **The Comune's street names differ from ours**: viale Augusto is VIALE OTTAVIO CESARE AUGUSTO
  in the SUAP lookup and VIALE DI AUGUSTO in the vault's stradario.
- **Asks the vault can't answer**: the name on the sign, the seating area (superficie di
  somministrazione), music, alcohol, start date. Alcohol was set to none for this walk.
- **Vault values need translating**: ditta_individuale → IMPRESA INDIVIDUALE, titolare →
  TITOLARE (a list of ~300 Registro Imprese roles), C/1 → C01, dates to gg/MM/aaaa, a street
  split into DUG and name.
- **The portal bundles more than the library says**: the insegna can join this pratica, and
  the relazione tecnica asseverata has no slot of its own (allegati liberi).

The drafts ("PROVA NON INVIARE - Pizzeria Fittizia SCIA" and "... (passo 2)") are kept in
Liam's Pratiche in bozza, unpaid and unsent.
