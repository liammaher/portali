# North star

Status: **draft, 2026-10-29.** It replaces the 2026-10-27 page, kept whole at
[`design/north-star-2026-10-27.md`](design/north-star-2026-10-27.md). What Liam said on
2026-10-29 is stated as decided; what follows from it and he hasn't confirmed is marked
*proposed*. The design rules in [`CLAUDE.md`](../CLAUDE.md) describe the code as it is and change
in the pull request that changes the code, not before.

## The north star

**Ask Claude to do any dealing with an Italian public office, and it does everything up to the
steps the law keeps for you, using your data without ever seeing or keeping it. Free and open
source, for every city and every portal in the end.**

Napoli and impresainungiorno come first. They are where the map is built and proved, not the limit.

You log in yourself (SPID, in the Claude desktop app's browser). Claude then picks the procedure,
checks what's missing, fills the portal, reads every value back and hands you a review page. You
make the declarations, sign, pay and send.

## What doesn't bend

1. **Free and open source, full stop.** No fee, no account, no server of ours that costs money to
   keep up. Anything that needs one is out, or it runs from static files in this repo.
2. **The person takes the human steps.** SPID login, declarations, signature, payment, sending.
   The runner never declares, pays or sends. "Anything" means anything up to there.
3. **All or nothing.** Between the login and those steps everything is automatic, or the case is
   refused before anything opens. A half-automated flow is worse than a manual one.
4. **Claude doesn't see the data it fills.** What this means, stage by stage, and where it isn't true
   yet, is in *The privacy contract* below.
5. **Honest coverage.** Each pratica is `available` (run on the real portal and unchanged since),
   `paused` (a portal section changed), `mapped` (not yet run for a client), `guide` (advice only) or
   `coming`. It comes from the repo (`plugin/build.py`, `plugin/coverage.yaml`), never typed by hand.
6. **Mapped on the real portals with made-up data.** No fake sites, no real pratiche to learn from.

## The first ten minutes

What the person should meet after installing, and what each step needs.

1. **Install in one step, with no terminal.** Today the README starts with `git clone` and
   `make build`: a developer's install. *(Not yet true; question 10.)*
2. **Ask in plain words**, in Italian or English. The first answer is whether it's supported:
   available, coming, or advice only.
3. **One message of questions**, with proposed answers to confirm (pre-flight does this).
4. **One sentence on privacy**: what Claude reads, and what goes straight to the form.
5. **"Log in with SPID, then tell me."** In the desktop app's browser.
6. **About a minute of run**, narrated in plain language, with no screenshot of a filled page.
7. **A review page**: what's left for you, and every value. You sign and send.
8. **A failure is specific and early.** A refusal says why and gives the manual steps; a changed
   portal can be reported as structure only (field labels and ids, never values). *(Reporting isn't
   built; question 11.)*

## The privacy contract

| Stage | What Claude sees | What it doesn't |
|---|---|---|
| You type your details in the onboarding page or `pvault set` | Nothing | Everything you type |
| You hand Claude a scan to fill the vault | That scan, once | Anything you didn't hand it |
| Pre-flight | The case's facts you told it, which vault fields exist or are missing | The values |
| Fill, in the separate window (`run.py run`) | The run's `report.json`: per section, saved and read back, pass or fail and why (`empty`, `different`, `missing`); when it stops, a `snapshot` of the page (each box's id, label and whether it holds anything) | The values: `pvault get` is piped into the runner after your approval (Touch ID, Hello or password), no report holds one, a portal message that repeats one is scrubbed, and Claude has no handle on that window |
| Fill, in the pane (`run.py page`) | The same report and snapshot; `run.js` as a sealed blob | The values: they travel sealed under a 15-character code that a window on your screen shows (not printed, not in a file, not in the arguments of anything Claude runs) and that you type into a box the runner opens on the portal's page. The box is masked and goes the moment the code is complete. No storage in the tab holds a value or the code in the clear. |
| A run that stops | The report and snapshot above; `__pv.look()` in the pane | The page itself: no screenshot is saved, and the skill forbids reading a filled page |
| Review | Nothing: the page is shown to you | `review.html` and `private/` (what each section typed, what a failed read-back wanted and found) are written for you; the skill sends the page and never opens either |
| Login | Nothing | Your SPID or CIE credentials, which are `personal` tier and which you type yourself |

**Where the promise doesn't hold** (found 2026-10-29):

- **The pane: fixed in transit, not on the page (2026-10-29).** `run.js` used to carry the values in
  the clear and Claude had to write them into a script call. Now `run.py page` seals them (`seal` in
  `run.py`, `unlock` in `inpage.js`; tests `a sealed run.js: ...`, `five wrong codes ...`, and the two
  resume cases), unless `--plain`. What is left: (1) once the box is gone the values are in the page
  and in the form, so reading a filled page is still a rule; (2) the code is on your screen, so a
  screenshot of your whole screen (computer use) would show it; (3) Cowork's sandbox has no window to
  show a code in, so the plugin uses `--plain`, and in that version Claude reads the documents
  anyway; (4) the window is written for macOS (tested), Windows and Linux (`zenity`), the last two
  untried; (5) sealed, a pane run's files weigh a third more than the 256 KB they count as.
- **Failure recovery: fixed the same day.** It used to have Claude read the failing screen
  (`read_page`), and `run.py` saved a full-page `failure.png`, either of which could show values.
  Now no report carries one (`inpage.js`: `redact`, `scrub`; tests `a stopped run's report, snapshot
  and stored copy hold no value` and `a portal label that repeats a value is scrubbed`), the
  screenshot is gone, and the recovery step works from the report and `look()`.
- **Browser reads after a fill: a rule in the pane, a fact in the window.** In the separate window
  Claude has no handle on the page. In the pane the skill forbids reading a filled page, and nothing
  enforces it.
- **Files on disk: a rule.** `review.html` and `private/` hold the values for the person. Claude's
  shell runs as the same user and could read them; the skill says to send the page and never open
  either. Both are mode 0600/0700, which does not stop that (and Windows ignores the mode).

## Where we stop

In: choosing the pratica, checking the case against the law and the portal's rules, collecting what's
missing, filling every section, attaching the files, reading everything back, the review page,
tracking.

Out, and the person's: the login, the declarations (false ones carry penal consequences for whoever
signs), a firma digitale, payments, sending, anything in person. Out of scope: what specialists
already do (taxes, payroll, invoices).

## What to map next

Not by city, by **pain density**. Until there are volume figures this is an estimate:

> runs a year × human minutes a run × share bounced or needing a second pass ÷ cost to map

Two things adjust it:

- **Reuse.** A section shared by many pratiche is worth more, because the dictionary
  (`dictionary/fnsuap.yaml`) is by section and not by pratica.
- **A gate: can the wizard be walked without submitting?** A portal with a draft or save step can be
  mapped with a scratch identity. One without can only be mapped by filing, so it waits for reports
  from users.

SPID is not a gate: the person logs in. The mapping unit is the platform. impresainungiorno is shared
by many comuni, so the next comune costs its rules, fees and offices; INPS, Agenzia delle Entrate and
the Questura each cost a new map. Repeat filers (geometri, architetti, commercialisti, small
businesses) come before one-off citizen procedures, then the high-volume citizen ones. Current
counts: `plugin/coverage.yaml` and `plugin/build.py`.

## How it stays current without a budget

*Proposed.*

- **Maps are data**: steps files, rules tables and the dictionary, generated by `gen.py` from a crawl.
  A fix is a small diff a contributor can review.
- **Drift is found by the people who run it.** The runner already stops on a changed section and
  writes `drift.json`. Sharing that as a structure-only report, opt-in, is the part to build.
- **CI runs offline**, with no secrets and no portal (`bench/test_*.py`). "Available" stays a status
  someone confirms on the real portal.
- **Demand is GitHub issues** ("request a pratica"), not a counting service.

## The 2026-10-27 decisions, and what happens to them

| # | Decision then | Now |
|---|---|---|
| 1 | Host the connector in the EU, as a GDPR processor | **Reversed.** Nothing of ours is hosted, so no processor role and no data-processing agreement. Local-first stands, and the README's "will change" note goes. Static files in the repo may carry coverage and updates. *(Follows from "free, full stop"; proposed.)* |
| 2 | Add PEC sending on an explicit tap | **Parked.** PEC stays read-only. Not part of the portal map. |
| 3 | One connector service in this repo | **Reversed.** `connector/` is optional; the plugin works without it (`design/packaging.md`). Not deployed. *(Proposed.)* |
| 4 | Design for owners, recruit intermediaries first | **Changed.** Design for whoever does the pratica; order by pain density. Intermediaries stay the heaviest users and best contributors. |
| 5 | Immigration is the second segment, with a partner | **Changed.** No segments: the ranking decides. The Questura isn't mapped. |
| 6 | No interim nudge before Duet | **Unchanged.** |
| 7 | The sensitivity tier changes, as one schema change | **Unchanged** and still to do. |
| 8 | Pick the name with the first partner; fund hosting from grants | **Reversed.** There is no hosting to fund. The name is Portali (question 9). |

## Open questions

Questions 1 to 7 keep their numbers from the old page, because the runner, the skill and the
rehearsals cite them. Details are in [`design/north-star-2026-10-27.md`](design/north-star-2026-10-27.md#open-questions).

| # | Question | Now |
|---|---|---|
| 1 | Portals need a browser, and the phone has none | **Answered for desktop**: the app's own browser, never Claude in Chrome. The phone still can't run a portal. Whether Cowork's browser can run the runner is untested (`design/packaging.md`, tests 1 and 2): **the first thing to do**. |
| 2 | Portal filings need a firma digitale | **Unchanged.** It's the person's step (*Where we stop*). The portal rule still needs adding to `signing.json`. |
| 3 | `human_only` is broader than the person's taps | **Unchanged**, still the right fix: name the tap a step needs. |
| 4 | More PDF layouts, or not | **Reduced**: only for the forms a portal or PEC takes as a filled PDF, filled by the runner. |
| 5 | Keys and taps once hosted | **Moot**: nothing is hosted. |
| 6 | Which pratica first | **Done in practice**: the SCIA and CILA rehearsals ran on the real portal. Next is by pain density. |
| 7 | Large attachments in the pane | **Open and sharper.** The planned fix was a host of ours serving each file for a moment; that is out. Choices: Cowork's own file tool (test 2), the Playwright window, or the person attaching. |
| 8 | **Filling in the desktop app's browser without Claude carrying the values** | **Partly done.** (b) is built: in the pane `run.js` carries the values sealed under a code the person reads in a window and types into a box on the page, so they never pass through the conversation. The filled page stays a rule, not a fact. (a), the window as the run for real data, still needs no code and leaves Claude no handle at all. (c), a local process that drives the app's browser instead of Claude: nothing known offers one. Open: whether Cowork can show the code window from where its Python runs. |
| 9 | **The name** | **Decided 2026-10-29: Portali**, before any release existed. The plugin, its marketplace and its skills are `portali` / `portali-*`, and the release zip is `portali-<version>.zip`. `pvault` keeps its name, and so do the repository and the pratiche-napoli library. Not checked: trademark, domain, or another product by that name. |
| 10 | **Install without a compiler** | **Stage one built, not released** (`docs/install.md`): a marketplace entry pinned to a release zip, a setup skill and installers that fetch the release's `pvault` and check it against checksums the plugin carries. Files fetched with `curl` aren't quarantined by macOS, so the first version needs no Apple developer account: unconfirmed until tried on a clean Mac. Windows' script is untested. Still open: creating the vault without a terminal (stage two), no system Python (stage three), one-click listing (stage four). |
| 11 | **Sharing a drift report** | **New.** Structure only, opt-in, as a PR or an issue. |
| 12 | **Responsible use** | **New.** A short page: not legal advice, the person signs, one person's own session and no bulk use. Check each portal's terms on automation. |

## Where the rest lives

- [`design/north-star-2026-10-27.md`](design/north-star-2026-10-27.md): the page this replaces, with
  the five jobs, the segments and the old decisions in full.
- [`design/packaging.md`](design/packaging.md): the plugin, still drafted around a hosted connector;
  it needs revising against this page.
- [`design/supported-flows.md`](design/supported-flows.md): what a supported pratica promises.
- [`../claude/skills/pratica/portal/README.md`](../claude/skills/pratica/portal/README.md): adding a
  pratica and the portal's quirks.
- [`../reports/`](../reports/): the strategy reports from before this change.
