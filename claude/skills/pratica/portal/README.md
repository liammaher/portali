# Portal runner: adding a pratica, running it, checking them all

How a pratica on impresainungiorno (the SUAP and SUE front office) goes from crawled to
filled, as the 23 SUE recipes did. The crawl itself is in `catalogue/README.md`; the
person-facing flow (pre-flight questions, the review page) is in the pratica skill.

## Adding a pratica

1. **Is it all mapped?** `catalogue.py count` says which crawled sections the dictionary
   fills. A section none fills needs a dictionary entry (`dictionary/fnsuap.yaml`; its header
   says what a field can carry). Read the section on the portal when the crawl kept no `text`:
   the yes/no questions and their order are only on the page (open a scratch draft: a steps
   file with the wizard only and no sections, "PROVA NON INVIARE - catalogo …").
2. **The recipe** (`rehearsals/<name>/recipe.yaml`): procedure, title, comune, sportello,
   crawl, operation (or a list of them), scelte, `first`, `asks`, `yours` (what's left to the
   person: declarations, attachments, payment, signing), `supports` (the cases refused, with
   why: all or nothing). Copy a sibling: `rehearsals/paesaggistica-ordinaria-fittizia`.
3. **Generate:** `gen.py <recipe> --out <dir>/portal-steps.generated.yaml`. It writes nothing
   if any field is unaccounted for. Regenerate every rehearsal after a dictionary or gen.py
   change and look at the diff: an unrelated steps file that changes is a regression until
   shown otherwise.
4. **Pre-flight** with made-up answers (`answers-p6.json`) and the P6 fixture's values
   (`rehearsals/cila-fittizia/fixture-p6.json` "fields" as a dict): `run.py preflight`.
5. **Run in the pane** (the pratica skill, step 2) and record each run in the rehearsal's
   README (`| Date | Browser | Result |`): what stopped it and what fixed it, then the
   filled run.
6. **Tests:** `bench/test_gen.py` (add a case for what the pratica taught), `test_preflight.py`,
   `test_inpage.py` when inpage.js changed.

## What the portal does that bit us

- **Labels.** A box is found by the start of one of its first three texts (its title, its
  cell, the row above, the cell to its left, the nearest label before it). In a row of boxes
  the nearest label before each is the *last* column's, so a label can name several boxes:
  the runner takes those whose own text it is first, and `nth` counts only those. "sì"/"no"
  radios under one another name each other: name them by the question beside them
  (`on_page` when the crawl labels them only "sì").
- **Boxes that move.** A tick that opens rows above the box renumbers it (the paesaggistica's
  art. 136): gen.py sends it by label. The crawl's `moved` pairs two same-label boxes wrongly
  at times; a pair whose new id was on the page already is not a move.
- **Section names.** Two sections can share a long prefix ("dichiarazione relativa alla
  conoscenza d…"): open and check by the whole name. The Scheda anagrafica's link reloads the
  whole page: the runner waits until the section is the open one before reading it.
- **Scelte.** Some sections exist only with a scelta ("ci sono cointestatari"): list every
  scelta the case needs; gen.py ticks them all.
- **Values the portal checks.** Codici fiscali and partite IVA by their check digit (the P6
  fixture's pass), the draft's name at most 60 characters, a P.G. box may take 10.
- **Required oddities.** The voltura's co-holder sex is two radio groups, each required: the
  section saves only with both ticked (the recipe says so in `yours`).
- **Acknowledgements** in the person's name are `theirs` in the dictionary (their wording
  varies by pratica: list each one), never ticked.
- **Answers change the attachments screen.** An onerosa CILA adds the contributo's prospetto
  and receipt slots; a title held alone drops the co-holders' IDs. A slot the crawl's screen
  hasn't is `on_page` in the recipe, found by label under its `when`.
  An asseverazione's "si allega" adds its own required slot, one per fascia di rispetto
  ("… tutela funzionale (stradale)"): run a new case to Allega documenti first, read the
  slots it adds, then map them (the SCIA, 2026-10-29).
- **Assents asked with a pratica.** On Napoli's SUE a SCIA condizionata (the Soprintendenza,
  a park, a vincolo) gives no start date, and an assent that is its own operation (a landscape
  authorization) is selected with the pratica in the wizard, not attached to it.
- **A block a tick opens may repeat the labels above it** (h1's structural designer under
  the architect's): fill it by label from its first box (`anchor`, `block: 1`).
- **Suggestion lists fade out.** A street's list, hidden by its widget, lies over the next
  boxes while it fades (PrimeFaces 11): the runner takes it off the page at once ("covered by
  li.ui-autocomplete-item" before, 2026-10-29).
- **A file box's cell names the file once it's in.** Name an upload by the row above it (the
  crawl's `context`), not by its cell ("Tipologie ammesse"), or the read-back can't find it.
- **The portal's own errors:** "errore inatteso", a 503 for maintenance, a fresh load that
  lands on `/cmd/error`, a section slow to open or to show its boxes. Every section is saved
  by then, so the runner picks itself up: it reopens the draft from Pratiche in bozza (or
  goes on from its address), waits 10 s and resumes, at most twice, each try in the report's
  `retries`. A value the portal refuses or doesn't keep, or a renamed label, still stops.
  Two runs on one login (another session's, a crawl) make these errors: one at a time.

## What pre-flight checks in a signed file

A `.p7m` is read with openssl (`p7m_signers`; PEM, DER and base64 text) before any browser
opens: unreadable is `not ready`; no codice fiscale in its certificates, or none of the
dichiarante's (`t:identity.codice_fiscale`), is a `warnings` entry. Counts and a yes/no only: Claude
reads the report, so no codice fiscale is in it. It does not verify the signature or the
certificate's validity at signing time; the portal does.

## What the person reads before they declare

The review page is where the person reads, not where Claude summarizes. `run.py run` adds two
things the portal itself says, copied by the program and never read by Claude:

- **The declarations.** After the fresh load, `readDeclarations` (inpage.js) opens each section
  the steps file leaves out (`DECLARATIONS`) by its menu link, copies the open section's text
  (`li#corrente`'s `innerText`) and each tick box's label and state, ticks nothing, and goes back
  to the section the steps left open. The text is held (`__pv.held().declarations`, may carry the
  person's name), written to `private/declarations.json`, and shown word for word. A box already
  ticked is flagged. A section that won't open is `declarations_note` in the report, not a stop.
- **The portal's PDF preview.** `capture_preview` presses "Visualizza anteprima" (`__pv.preview()`),
  fetches the PDF the button opens (a window, a blob or a download) in the person's own browser,
  and keeps it as `private/anteprima.pdf`; the page draws its pages when PyMuPDF is there and always
  offers the PDF to download. Not a PDF, or no window: `preview_note`, and the page says to open it in
  the portal.

The pane (`run.py page`) copies neither: the text would pass through Claude. The page then says so.
**Not yet seen on the real portal:** both are proven on the bench's fake portal only. First real run:
check that `li#corrente` holds a declaration's whole text, that a section's tick box labels are its
own, and what Visualizza anteprima opens (a PDF at an address, a blob, a download, or the frame
itself), then record it here and in the map (`seen:`).

## Made-up data and the code window

`run.py page` seals `run.js` and opens a window with the code, which the person types into the
portal's page: right for a real person's data, a nuisance for a rehearsal. Rehearsals with made-up
data pass `--plain`; `bench/rerun.py` builds its scripts without a code. Where no window can open
(a sandbox, a remote shell) `page` stops and says so.

## Checking them all: `bench/rerun.py`

After a change to inpage.js, the dictionary or gen.py, rerun the rehearsals in the pane, one
after another, nothing touched between them:

```sh
~/.cache/pratica-portal/venv/bin/python claude/skills/pratica/portal/bench/rerun.py \
    --values <values.json> --out <dir> [rehearsal ...]
```

In the pane, on the portal's origin and logged in: `check.js` (it names which of `runner.js`
and `steps/<name>.js` the pane lacks: send only those), the `queue-N.js` files, `loop.js`,
then `results.js` until `done` equals `of`. A stopped one: rerun it alone, unchanged, before
calling it a bug (the portal is sometimes slow); twice the same is a bug. 2026-10-29: 20 of
23 on the first pass, 23 of 23 once the runner waited for each section to open.

## Attachments screens

`__crawl.slotsOf("<draft name>")` (crawl.js) opens a draft from Pratiche in bozza and reads
its Allega Documenti slots from the menu at the top, so the sections need not be complete;
an empty "catalogo" draft gives a pratica's base slots (those its answers add are missing).
A slot is marked `required` only once the portal has checked the screen.
