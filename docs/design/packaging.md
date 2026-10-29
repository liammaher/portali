# Packaging: an architect's own Claude, on a $20 plan

The user: an architect in Naples with Claude Pro. They know what the client wants and which
pratiche it needs, and they have the client's data and documents. They want to hand all of it
to Claude and get back a finished draft on impresainungiorno, checked, with only their own steps
left: logging in, the declarations, signing and sending (`supported-flows.md`). This is how what
we built reaches them. `napoli-portal.md` is what it takes to cover the platform; this is how to
ship it.

Status (2026-10-29): proposal. Nothing here is built; two tests decide it (below).

## What we bring that Claude doesn't have

Claude can already open the portal and fill in boxes. Without us it reads the page and acts one
box at a time, a model call each: hundreds of calls for a SUAP pratica, any of which can go
wrong, and much of a Pro plan's 5-hour allowance (unmeasured; test 3 measures our way). What
we add:

- **The map:** every section's boxes by label, what each choice opens, and which boxes
  renumber (`catalogue/`, `dictionary/fnsuap.yaml`).
- **A runner:** `inpage.js` fills a whole pratica from a steps file with no model calls, and
  reads every value back after each save and again on a fresh load of the draft. The SCIA
  takes 53 s from login to saved draft in the pane (`rehearsals/pizzeria-fittizia/`).
- **The law:** which pratica a case is, the others it adds, and the cases we refuse before
  anything opens (`rules/`, `run.py preflight`).
- **Upkeep:** a changed section stops runs (drift, `catalogue.py`), and we map it again once
  for every user. One architect can't do that for themselves.

The first two are about speed, accuracy and the cost of a run, and they matter now. The last two
are what stays ours as Claude gets better at portals by itself: it won't learn Naples's rules,
or notice that InfoCamere changed a section last Tuesday.

## Where it runs: Cowork, with its own browser

On 2026-08-26 Cowork gained a browser built into the Claude desktop app. It's on Pro, Max and
Team, on macOS and Windows (Linux in beta), and separate from the user's own browser. Logins
in it persist between sessions on the same computer. It opens sites, reads pages, clicks and
fills forms
([Anthropic](https://claude.com/resources/articles/cowork-built-in-browser)). Plugins
(skills plus connectors) are on every paid plan, installed from a file or a marketplace
([help](https://support.claude.com/en/articles/13837440-use-plugins-in-claude-cowork)). Custom
remote connectors are on Pro
([help](https://support.claude.com/en/articles/11175166)).

So north-star's question 1 ("a browser agent, which today is Claude in Chrome") has a better
answer for desktops: the app's own browser, which is where our runner already runs, in the Code
tab. Claude in Chrome stays out.

| Surface | Fills the portal | For an architect | Verdict |
|---|---|---|---|
| **Cowork + plugin + connector** | in the built-in browser, if tests 1 and 2 pass | folders and chat, no setup beyond the plugin | **the product** |
| Code tab (today) | yes, `run.py page` in the pane | worktrees, permissions, a shell: a developer's tool | fallback, and where we test |
| claude.ai chat + connector | no browser | advice only | not for portal pratiche |
| Our own app (Playwright window, `run.py run`) | yes, fastest, any file size | software to install, outside Claude | only if the Cowork tests fail |

## The package

**1. A "Napoli" plugin.** The pratica skill and its playbooks, cut down to what an architect's
job needs: read the client's files, call the connector, run the browser part, show the review.
It holds no portal map of its own (that drifts); it gets the map from the connector.

**2. A hosted connector with no personal data** (decision 1's first step, decision 3: one
service in this repo reading the library's JSON). Its tools:

- `preflight(case)`: the case's facts in, out comes whether it is supported, which pratiche it
  needs (the rules table), the questions to ask in one message, the documents and data
  missing, and the files to attach with their limits.
- `steps(recipe, answers)`: the steps file `gen.py` writes, and `install.js` (inpage.js and
  the steps, no personal data, as `run.py page` already splits it).
- `check(values)`: the format rules (codice fiscale, dates, amounts) and address →
  municipalità, quartiere, distretto (`internal/napoli`), without storing anything.
- `drift(pratica)`: whether the map is current, so a changed pratica is refused, not
  half-filled.

The person's values never reach it: Claude builds `run.js` (a few lines: the values and a call
to the runner) in the conversation, from the client's documents.

**3. No vault in the first version.** The client's documents stay in the Cowork folder for that
job; Claude reads the data out of them, `check` validates it, and nothing about the client is
kept on our side. The vault comes back with what needs it: repeat clients, retention, the sign
gate, PEC, once hosting is designed (decision 1's second step, north-star question 5).

## One job, as the architect sees it

1. They put the client's files in a folder (visura, documento d'identità, planimetria,
   relazione) and write one line: "SCIA per ristorante in via X, cliente Y, con dehors".
2. Claude reads the files and calls `preflight`. The architect answers one message: the
   questions, and the answers Claude proposes for them to confirm.
3. The browser opens on impresainungiorno; they log in with SPID.
4. Claude runs `install.js`, then `run.js`: the wizard, every section, the attachments, the
   read-back, the fresh load.
5. They read the review page (`review.py`): what's left for them and every value.
6. They make the declarations, sign the procura and the modules with their firma digitale,
   and send.

Nothing between 3 and 5 comes back to them (`supported-flows.md`); a case we can't finish is
refused at 2.

## Two tests before building anything

**1. Can Claude run a script in Cowork's browser?** The runner needs one JavaScript call per
script, as the pane's `javascript_exec` gives today. The announcement doesn't say. If it's the
same browser tool as the Code tab, `run.py page` works unchanged. Test: in a Cowork session,
open the portal, run the SCIA rehearsal's `install.js` and `run.js`, compare `report.json`
with the pane's. If there is no script call, the fallback is filling box by box from the steps
file with the form tool: still no guessing, but many calls, which the third test then has to
price.

**2. Do attachments of a real size get in?** In the pane, `run.js` carries file bytes through
the conversation, at most 256 KB in all (north-star question 7). A planimetria is 1–5 MB, and
the portal takes up to 9.77 MB a file. If the architect has to attach files by hand, the flow
isn't all-or-nothing any more. Test: Cowork's file box behaviour, then question 7's fix (the
connector serves each file once at a short-lived `https` URL, CORS limited to the portal). That
fix puts a document on our server for a moment, so it waits for the hosting design, or is the
first thing that design covers.

Then measure:

**3. What a pratica costs on Pro.** Chat, Cowork and the browser share one allowance, and browsing
uses it faster than chat. Count the tokens of the SCIA rehearsal in Cowork, from the first message to
the review page; the target is several pratiche in one 5-hour window.

## Things to settle

- **The client's data.** The architect is its controller; Anthropic advises against other
  people's personal data in the browser. The plugin says exactly what goes where (the connector
  gets none in this version) and tells the architect to turn off training on their account.
- **The procura speciale.** The portal generates it for each pratica; local practice differs on
  whether it's signed by hand with an ID scan or digitally
  ([SUAP Molise](https://suap.regione.molise.it/faq/posso-allegare-alla-pratica-suap-una-procura-non-firmata/printable/pdf),
  [Unioncamere Veneto](https://www.unioncamereveneto.it/wp-content/uploads/2021/10/20211006-Suap-Veneto-5.pdf)).
  Find what Napoli's SUAP and SUE accept, put it in the library's `contract/signing.json`
  (north-star question 2), and have the review page say which.
- **Values without the vault.** `run.py` takes values from `pvault get`; the plugin's path
  takes them from the documents. `plan`'s value resolution needs an input that isn't the vault,
  checked the same way.
- **Distribution.** A plugin file first, sent to the first architects; a marketplace once there's
  more than one plugin version to keep straight.
