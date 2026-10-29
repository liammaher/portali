# Packaging: an architect's own Claude, on a $20 plan

The user: an architect in Naples with Claude Pro. They know what the client wants and which
pratiche it needs, and they have the client's data and documents. They want to hand all of it
to Claude and get back a finished draft on impresainungiorno, checked, with only their own steps
left: logging in, the declarations, signing and sending (`supported-flows.md`). This is how what
we built reaches them. `napoli-portal.md` is what it takes to cover the platform; this is how to
ship it.

Status (2026-10-29): a draft plugin (`plugin/`) and its connector (`connector/`) are built
and tested offline; the connector isn't deployed and nothing has run in Cowork yet. Two tests
decide the run (below); the plugin's test skill runs them.

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

So portal pratiche run in the Claude desktop app's own browser, where our runner already runs in
the Code tab (north-star question 1). Never Claude in Chrome, the extension in the user's own
Chrome: not as a fallback, not as Cowork's "preferred browser" setting. The plugin tells the
user to keep that setting on the built-in browser.

| Surface | Fills the portal | For an architect | Verdict |
|---|---|---|---|
| **Cowork + plugin** | in the built-in browser, if tests 1 and 2 pass | folders and chat, no setup beyond the plugin | **the product** |
| Code tab (today) | yes, `run.py page` in the pane | worktrees, permissions, a shell: a developer's tool | fallback, and where we test |
| Chat, web or phone, with the plugin | no browser | what's supported, which pratica, what the law asks (the connector) | the questions, not the runs |
| Our own app (Playwright window, `run.py run`) | yes, fastest, any file size | software to install, outside Claude | only if the Cowork tests fail |

## The package: a listed plugin and its connector

The setup never changes: the user's own Claude, on a $20 plan, and their data. What we bring
changes every week (new pratiche, comuni, avatars; a portal that moved a box), so it is split
by one rule: **code that touches the client's data runs on the user's computer (the plugin);
knowledge that touches no one's data lives on our server (the connector).** This is the shape
Anthropic's docs give a product: a plugin whose skills teach the workflow, pointing at a
remote MCP server, listed together in the directory
([what to build](https://claude.com/docs/connectors/building/what-to-build)).

**The plugin** (`plugin/`, `plugin/README.md`, built by `plugin/build.py`):

- **The skill:** the job from the client's documents to the review page, and the rules that
  never bend (all or nothing; the architect's declarations, signature, payment and sending
  stay theirs; no credentials; the built-in browser).
- **The runner and the map:** `run.py`, `inpage.js` and every rehearsal's steps file (44,
  23 of them SUE), as JSON, so Cowork's Python runs them with no packages.
- **The checks:** `check.py`, the vault's format rules for what the portal reads (codice
  fiscale, partita IVA, CAP, provincia, dates, the fixed lists), against `schema.json`.
- **The law and drift, as of the build:** the rules tables, the library, `drift.json`
  (pre-flight refuses a pratica found changed) and `coverage.json`, so it works with no
  connector too.

**The connector** (`connector/`, `connector/README.md`): remote MCP, read-only, no sign-in,
stateless, serving the same build. `coverage` (what's supported, by comune and avatar),
`find_pratica` (a case's facts to the pratiche that fit, and the words nothing covers),
`get_pratica` (status, refusals, questions, the user's steps, and whether the plugin's steps
file is current: a stale one is replaced from it for that run) and `get_procedure` (the
library entry, with sources). Its instructions carry the rules to every session. Its tools
take a case's facts and never a name, codice fiscale or address, so the client's data goes
from the folder to the portal and to Anthropic, and never to us: no processor role, no
personal data to host.

Why both: the steps files stay in the plugin because each is 20–46 KB, 10–15k tokens if it
crossed the conversation on every run; the connector sends one only when the plugin's is
stale. The connector is what makes it current everywhere: a status or a new tool reaches
every user at once, with no plugin release, on every surface (chat on the web and the phone
answer "what can you do?" and "what does a CILA need?"; Cowork does the runs). A listed
plugin's new versions are scanned and served to everyone who has it
([publish](https://claude.com/docs/directory/publish.md)); one added from a GitHub
marketplace has auto-update off by default
([plugins](https://code.claude.com/docs/en/discover-plugins)), and an uploaded file never
updates. So the directory, not our own marketplace, is the channel.

**What's supported is never typed by hand.** `coverage.json` comes from the repo:
`available` (a rehearsal run on the real portal, its README's runs table, unchanged by the
last drift check), `paused` (the drift check found it changed), `mapped` (a steps file not yet
run on the portal: coming soon, never run for a client), `guide` (a library procedure with no
steps file: advice only) and `coming` (`plugin/coverage.yaml`, the one hand-kept list, with
the comuni and who files each sportello's pratiche). The landing page, the connector and the
listing all read it. 2026-10-29: 38 available, 6 mapped, 22 guide.

The user learns what to do from the product, at the moment it matters, not from a manual:
the listing and the first job (`studio.json`, once); every request (pre-flight and
`get_pratica`'s one line: go, paused, coming soon, or advice only); and "what can you do?"
anywhere (`coverage`). An MCP App could show that as a card in the conversation, and later
the review page.

**No vault in the first version.** The client's documents stay in the Cowork folder for that
job. `studio.json` at the folder's root holds the studio and the architect (`business.*`, the
architect's `ruolo.tipo` titolare, identity, residenza, albo), written once; each job's
`values.json` holds its people, in the vault's field ids (`bianchi:identity.first_name`), and
`check.py` validates and merges the two into what `run.py --values` takes, as `pvault get`'s
JSON. The vault comes back with what needs it: repeat clients, retention, the sign gate, PEC,
once hosting is designed (decision 1's second step, north-star question 5).

**The gap it shows:** the runner fills 23 SUE pratiche, but the library describes only one
of them (the CILA), and the rules table covers only the SUAP. For the other 22 the plugin
knows the steps file's asks, refusals and the architect's steps, not the law behind them;
the skill says so rather than answer from memory. Library entries for the SUE family are
the next content work.

## One job, as the architect sees it

1. They put the client's files in a folder (visura, documento d'identità, planimetria,
   relazione) and write one line: "SCIA per ristorante in via X, cliente Y, con dehors".
2. Claude finds the pratica (`find_pratica`, `get_pratica`: available and current), reads
   the files, writes `values.json`, runs `check.py` and pre-flight. The
   architect answers one message: the questions, and the answers Claude proposes for them
   to confirm.
3. The browser opens on impresainungiorno; they log in with SPID.
4. Claude runs `check.js`, what it names, then `run.js`: the wizard, every section, the
   attachments, the read-back, the fresh load.
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
the conversation, at most 256 KB in all (north-star question 7): Claude writes every byte of
the call, so megabytes never go that way. A planimetria is 1–5 MB, and the portal takes up to
9.77 MB a file. If the architect has to attach files by hand, the flow isn't all-or-nothing
any more, so until this passes the skill refuses a case whose files don't fit. Test: whether
Cowork's browser has a tool that puts a file from the folder into a page's file input (the
test skill tries a 3 MB PDF). If it has, the runner pauses at each file slot for that tool. If
not, question 7's fix: a small endpoint serves each file once at a short-lived `https` URL,
CORS limited to the portal; it puts a document on our server for a moment, so it waits for the
hosting design, or is the first thing that design covers. The runner side of that fix is built
and run end to end in the Code tab's pane with a 5 MB drawing from a test host (north-star question
7, 2026-10-29): what's left is the real host, and the same run in Cowork's browser.

Then measure:

**3. What a pratica costs on Pro.** Chat, Cowork and the browser share one allowance, and browsing
uses it faster than chat. Count the tokens of the SCIA rehearsal in Cowork, from the first message to
the review page; the target is several pratiche in one 5-hour window.

## Things to settle

- **The client's data.** The architect is its controller; Anthropic advises against other
  people's personal data in the browser. The plugin says exactly what goes where (nothing to
  us: the connector takes only a case's facts) and tells the architect to turn off training
  on their account.
- **The procura speciale.** The portal generates it for each pratica; local practice differs on
  whether it's signed by hand with an ID scan or digitally
  ([SUAP Molise](https://suap.regione.molise.it/faq/posso-allegare-alla-pratica-suap-una-procura-non-firmata/printable/pdf),
  [Unioncamere Veneto](https://www.unioncamereveneto.it/wp-content/uploads/2021/10/20211006-Suap-Veneto-5.pdf)).
  Find what Napoli's SUAP and SUE accept, put it in the library's `contract/signing.json`
  (north-star question 2), and have the review page say which.
- **Distribution.** The `.plugin` file first (`plugin/build.py`), for the Cowork tests and
  the first architects; then the directory: the plugin from a public repo and the connector
  as its own listing, paired, so one "Add" gives both and every release reaches everyone.
  The plugin's name should outlive Naples ("napoli" today).
- **Hosting the connector.** EU, stateless, behind `GET /healthz`; it serves `plugin/dist`
  as built from main, so a merge redeploys both. The listing's checklist adds tool
  annotations (done), Origin checks (done), a privacy policy and documentation.
- **What's next, from demand.** A `request` tool (an anonymous count per pratica, comune and
  avatar) would order `coming`; it needs a store, so it waits for the hosting.
- **`check.py` against the vault.** It ports the format rules the portal's fields need, not
  all of `NormalizeAndValidate` (housing, immobile and pratiche rules, located fields). A test
  holding the two to the same answers belongs with the next schema change.
