---
name: portali-pratica
description: Prepare a Comune di Napoli pratica on impresainungiorno (the SUE and SUAP front office) from a client's documents - CILA, CIL, SCIA, agibilità, autorizzazione paesaggistica, inizio and fine lavori, proroghe, volture, dehors, a shop or restaurant opening - as a checked draft that stops before the architect's declarations, signature, payment and sending. Also answers which pratica a Naples case needs, with what documents, costs and timelines. Use when an architect or geometra asks to prepare, fill, draft or file a pratica for a client in Napoli, or asks what a case needs.
---

# Napoli pratiche

The user is an architect (or geometra) in Naples filing for clients with a procura. You
prepare the draft on impresainungiorno; they log in with their own SPID, then make the
declarations, sign, pay and send.

If they would rather keep their own or their clients' details in an encrypted vault on their
own computer than in a folder of files, the `portali-setup` skill installs it.

Every path below is relative to this skill's base directory: write it out in full in each
command (`SKILL` below stands for it). Run scripts with `python3`; they need no packages (each finds its own folder's modules first).

- `catalog.json`: each pratica with a steps file: `name`, `procedure`, `title`, `sportello`,
  `refuses` (the cases it doesn't cover, and why), `notes` (the steps file's own notes: who
  the roles are) and `steps_sha256`.
- `coverage.json`: every pratica's `status` when this plugin was built (`built`), and the
  library procedures it can only advise on.
- `pratiche/<name>.json`: its steps file.
- `library/`: the pratiche-napoli library. `procedures/` says what the law asks for each
  procedure (documents, costs, deadlines, offices, with sources), `rules/` which pratica a
  case is, `offices/` where to go.
- `reference/schema.json`: every field id, with what it means.
- `check.py`, `portal/run.py`, `review.py`: the scripts.

## Rules that never bend

- **All or nothing.** Between the login and the architect's own steps the run does
  everything, or the case is refused before anything opens. Never fill a box by hand to get
  past a stop, and never call a half-filled draft done.
- **The architect's taps stay theirs.** Never tick a declaration, press pagoPA, Invia or
  Firma, sign or send. The runner refuses them; don't do them by hand.
- **No credentials.** Never ask for, read or write a password, PIN or SPID/CIE login, the
  architect's or a client's. The architect logs in to the portal themselves.
- **No guesses.** Every value comes from a document in the folder or from the architect.
- **The data stays in the folder.** Write values only into the job's folder; send them
  nowhere but the portal, through the run.
- **The built-in browser.** Use Cowork's own browser for the portal, never Claude in
  Chrome. If the browser setting points at Chrome, ask the architect to switch it.

## The folder

The folder the architect opened in Cowork holds:

- `studio.json` at its root: the studio and the architect, kept for every job. `business.*`
  is the studio; the architect is a person with a slug (`rossi:`): `rossi:ruolo.tipo` is
  `titolare`, then `rossi:identity.*`, `rossi:residenza.*`, `rossi:albo.*`. None yet: build
  it from what they have (codice fiscale, tessera dell'ordine, the studio's visura) and ask
  for the rest in one message.
- a subfolder per job, with the client's documents. You write there: `values.json` (this
  job's people), `answers.json` and `run/`.

## What's supported, and the Portali connector

A pratica's status says what you may do with it:

- `available`: run on the real portal and unchanged since. Prepare it.
- `paused`: the portal changed it; pre-flight refuses it until it's mapped again.
- `mapped`: its steps exist but it hasn't run on the portal yet. It's coming soon: don't run
  it for a client.
- `guide`: the library says what it needs and where; it can't be filled automatically.
- `coming`: being worked on.

The plugin may bring the **Portali connector** (its `coverage`, `find_pratica`, `get_pratica`
and `get_procedure` tools). It holds no one's data and must get none: describe a case to it
by its facts only (the works, the activity, the size), never by a name, codice fiscale or
address. When it's connected, its statuses are the current ones; use it for "what can you
do?" (`coverage`, with the user's comune and role) and to find the pratica. When it isn't,
use `coverage.json` and say its `built` date.

## 1. Which pratica

Read the request and the job's documents. Find the pratica (`find_pratica` with the case's
facts, else `catalog.json` and `coverage.json`); a case can need two (a restaurant and its
dehors). Then, with the connector, call `get_pratica` with its `name` and the
`steps_sha256` from `catalog.json`:

- a status other than `available`: tell the architect in one line what it means and stop.
- `copy: stale`: the portal map changed after this plugin was built. Call it again with
  `include_steps`, save the steps file it returns as `run/steps.json`, use that path in
  place of `SKILL/pratiche/<name>.json` below, and tell the architect to update the plugin.

Read its `library` file (or `get_procedure`) for what the law asks. Many entries have none yet (the
runner fills more pratiche than the library describes): then the entry's `notes`,
`refuses` and its steps file's `asks` and `yours` are all this plugin knows, so say that
rather than filling in the law from memory. When none fits, say in one line which pratica
it is and that this plugin can't prepare it yet, give what the library says it needs, and
stop: don't open the portal. When two could fit, ask once, with the difference in a
sentence.

## 2. The values, from the documents

The steps file reads field ids. `t:` is the architect (the titolare in `studio.json`); the
job's other people are roles, which `answers.json` names under `people` (the entry's
`notes` say which: `{"people": {"c": "bianchi", "o": "immobiliare"}}` makes `c:` the client
`bianchi`). Write each person's fields into `values.json` as `"bianchi:identity.first_name":
"Mario"`, with their `bianchi:ruolo.tipo` (`cliente`, `impresa_esecutrice`, ...). Slugs are
lower case, letters, digits and hyphens. `reference/schema.json` says what each field is.

Then check and merge, from the job's folder:

```sh
python3 SKILL/check.py ../studio.json values.json --out run/values.json
```

Each line it prints is a value to fix: read the document again, or ask.

## 3. Pre-flight, before anything opens

```sh
python3 SKILL/portal/run.py preflight --steps SKILL/pratiche/<name>.json \
  --answers answers.json --values run/values.json
```

It prints JSON and exits 0 only when `ready`. Write file answers as paths from the job's
folder.

- `unsupported`: this case isn't covered (the reason is given). Tell the architect in one
  line and stop.
- `missing`: fields not in the values. Look for them in the documents first.
- `questions` and `proposals`: ask everything still open **in one short message**,
  numbered, each `question` as written (with its `options`; a `how` as a short hint), plus
  the missing fields no document gives. A `checklist` is one question with its items as
  bullets (usually "none": write the ticked keys as a list under its `key`, `[]` for none);
  an `only_if` question goes under its items. Proposals go in the same message as "I'll
  use: ...", one line each; "ok" confirms them all. Never use a proposal nobody confirmed.
  Write the replies under each `key` (dates YYYY-MM-DD) and run pre-flight again; an
  `error` says why an answer didn't fit, so ask only that again.
- `files`: each attachment. In this version a run carries its files through the
  conversation, so all of them together must fit in 256 KB (`pane_ok`). When they don't,
  stop: tell the architect in one line that files that size can't go in automatically yet.
- `yours`: tell the architect now what they'll do at the end.

Only when it says `ready` go on. From here the architect hears nothing more from you until
the review page, except the login.

## 4. The run

```sh
python3 SKILL/portal/run.py page --steps SKILL/pratiche/<name>.json \
  --answers answers.json --values run/values.json --out run --plain
```

`--plain` because in this version you read the architect's documents and wrote
`run/values.json` yourself, so the values are in this conversation already, and Cowork's
sandbox has no window to show a code in (`page` without it seals `run.js` under a code the
person types, and refuses when it can't show one).

1. Open `https://suap.impresainungiorno.gov.it/fnsuap/cmd/home` in the built-in browser and
   ask the architect to log in with SPID there (the login stays for later sessions). Wait
   until the page is the portal's own, past the login.
2. Run each script as **one** JavaScript call on that page, its text exactly as the file
   has it: `run/check.js` first. It returns what to run next: `runner.js` and `steps.js`
   only when the browser doesn't hold them already (never send one it doesn't name), then
   `run.js`.
3. A call ends after about 45 s, so `run.js` returns at once: call `await __pv.wait(40000)`
   until `result` is `filled` or `stopped`.
4. Save the report: the value of `JSON.stringify(__pv.last())`, written to
   `run/report.json`. Then `python3 SKILL/review.py run` writes `run/review.html`.

One run per login at a time: the portal keeps one pratica open per login, so a second run
in another tab moves the first one's draft.

**If it stops, recover it without the architect:**

1. `__pv.last()` has `error` (what differed), `at` (the wizard screen, or the section and
   field) and `snapshot`, the page with no value in it (each visible box's id, label and
   whether it holds anything); `__pv.look()` gives it again. Use those and nothing else:
   never read, screenshot or take the text of a page the run has filled, and never open
   `run/review.html` yourself (show it). A failed read-back says `empty`, `different` or
   `missing`, never what was typed.
2. "0 fields with this label" usually means the section wasn't open yet, not that the label
   changed: check `__pv.look()` before fixing anything.
3. Work out the fix for that one field: its new `id` or `css`, or the `label` the page now
   shows, and call `__pv.resume({fixes: [{"section": i, "field": k, "set": {"label": "..."}}]})`.
   It carries on on the same draft and checks every section again at the end.
4. A draft whose address expired (`/cmd/error`): `url = await __pv.reopen("<draft name>")`,
   then `__pv.resume({url})`. The portal's own errors ("errore inatteso", a 503) are the same.
5. Ask the architect only when the page wants a fact the documents don't hold: one plain
   question, then resume. Never fill a guess.
6. Budget: two fixes or about three minutes. Past that, stop, leave the draft saved, and
   say plainly what's left. Tell them which fix you made, so it can be reported.

## 5. Hand over

Show `run/review.html`: what's left for them, every check, every value as it went on the
portal. `__pv.open()` shows the draft. List their steps (`yours`) once more, in order. Then
nothing else: they review, declare, sign, pay and send.

## Questions with no run

"What does a CILA in the centro storico need?", "Is this a CILA or a SCIA?": answer from
`get_procedure` (else `library/procedures`) and `library/rules`, naming the source each fact cites. Say when the
library doesn't cover it rather than filling the gap from memory.
