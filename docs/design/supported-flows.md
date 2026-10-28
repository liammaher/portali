# Supported flows: one message of questions, a login, a finished draft

The product is only worth using if a pre-mapped pratica is faster and more accurate than a
person doing it. For each flow we support, the promise is narrow and strict: **Claude asks
every question in one message, the person logs in, and the next thing they see is a
finished draft, checked, with their remaining steps.** The person never hears about the
web page. Their only steps are the ones the law keeps for them: logging in, the
declarations, paying, signing and sending.

What already holds (the SCIA rehearsal, `rehearsals/pizzeria-fittizia/`): a mapped run makes
no model calls, takes 45 s from login to saved draft in a Playwright window (53 s in the
browser pane), checks each field against its label, reads every value back after each
save and again on a fresh load of the draft, and stops on anything it doesn't expect. Three
pieces are missing. Pieces 1 and 2 share a new section of the steps file, so they are built
together; piece 3 comes after.

Status (2026-10-28): pieces 1 and 2 built and retrofitted onto the SCIA (`run.py preflight`,
`asks:` and `supports:` in `rehearsals/pizzeria-fittizia/portal-steps.yaml`, tests in
`claude/skills/pratica/portal/bench/test_preflight.py`); the zona is still asked, not looked up.
Piece 3 not built.

## 1. Pre-flight: everything settled before anything opens

- **`asks:` in the steps file.** Each `{{ask.x}}` gets its question in plain words, a type
  (date, number, yes/no, choice, text, file) and why it's needed. Yes/no questions go in a
  `groups:` checklist, each phrased as the exception, so one question covers them all and
  "none" is the usual answer. Every question must be read by a field, a wizard screen or
  `supports` (pre-flight refuses one that isn't), and a question only a skipped field
  reads isn't asked: the SCIA asks one checklist and two short answers.
- **`run.py preflight`**, before any browser opens, returns JSON:
  - `questions`: the asks not yet answered, in order, with their wording;
  - `missing`: vault fields the steps need that are empty or invalid (an expired document
    counts), each as a question or a "fill this in the vault" item;
  - `files`: each file to attach, whether it exists, and the total against the pane's 256 KB;
  - `unsupported`: why this person's case isn't mapped (piece 2), checked first;
  - `yours`: the person's own steps at the end, so they hear them before starting.
- **The pratica skill**: run pre-flight, ask its questions in one message, write the answers,
  run pre-flight again until it comes back clean, and only then open the portal.
- **Bench cases** (`claude/skills/pratica/portal/bench/`): an unanswered ask becomes a
  question, a missing vault field becomes a question, a missing file, too much for the pane,
  a clean pre-flight.

## 2. Branch coverage: never start a flow we can't finish

A steps file maps one path through the portal. The SCIA's is a ditta individuale in zona C
with no alcohol, no music and no outside sign; a srl or zona A takes other screens.

- **`supports:` in the steps file**: conditions in the same template language as `when`,
  each with a plain message for when it fails ("only ditte individuali so far").
- **Questions that pick a branch** (alcohol? music? an outside sign?) are asks too. A "yes"
  that leads to an unmapped branch stops at pre-flight with that message, not halfway
  through the portal.
- **Facts the library works out but the vault doesn't hold** (the zona, from the Piano's
  street list) need a lookup before they can be checked; until then pre-flight asks.
- **The "supported" bar**: a flow and branch is offered only after three clean rehearsals in
  a row across fictitious variants, plus its bench tests, recorded in the rehearsal
  README's runs table.
- **Bench cases**: a supported case, an unsupported forma giuridica, a branch-picking "yes".

## 3. Self-recovery: a stop never becomes a conversation

For the browser pane first, the product's browser, where Claude has its tools.

- **Resume in `inpage.js`**: `__pv.start(procedure, values, {from})` starts at a given wizard
  step or section, and the report says exactly where a run stopped.
- **When a run stops**, Claude recovers it without involving the person: read the page as
  text, finish just that screen from the map and the values already gathered, resume the
  runner from the next section, and let the fresh-load check cover everything at the end,
  the hand-done screen included.
- **Limits**: the same refusals (declarations, payment, signing, sending). A genuinely new
  required field the map doesn't know is the only reason to ask the person, in one plain
  question. Time budget: two screens or about three minutes; past that, Claude stops, keeps
  the draft and says plainly what's left.
- **A drift report** next to the run's report: what changed and what Claude did. We fold it
  into the steps file and the map, and the flow leaves "supported" until it passes three
  clean rehearsals again.
- **The Playwright window**: recovery there needs Claude attached to the window (Chromium's
  debugging port plus the Playwright CLI), which is unverified, so it comes after the pane.
- **Bench case**: a portal whose label changes partway; the run stops, a scripted fix stands
  in for Claude, the run resumes, and the fresh load passes.

## Order

1. Pre-flight with `asks:` and `supports:`, retrofitted onto the SCIA, with its bench cases.
   Small: mostly `run.py`, the steps file and the skill.
2. Resume in `inpage.js`, the recovery steps in the skill, the drift report. Medium.
3. GEIS (the imposta di soggiorno), built to this standard from its first walk.

The test of the whole promise, against an architect and an independent answer key, is
pre-registered in [`rehearsals/cila-fittizia/README.md`](../../rehearsals/cila-fittizia/README.md).

## Not covered

Pre-flight can't check that the person's SPID works until they log in, or that the portal
is up. Attachments over 256 KB in the pane wait for open question 7 in
[`north-star.md`](../north-star.md); a firma digitale for portal attachments is open
question 2 there.
