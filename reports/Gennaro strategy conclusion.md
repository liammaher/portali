# Gennaro the pizza guy: what solves his problem, and where we fit

Conclusion of the 2026-10-27 strategy discussion. Gennaro runs a pizzeria in the Comune di Napoli as a
ditta individuale with 3 employees, a dehors and an insegna. He has a commercialista and a consulente
del lavoro, lives on his phone, and won't pay anyone to set up tools.

## What would completely solve it

He never deals with paperwork. His assistant (Claude, Meta Muse, whatever he uses) knows everything he
owes, national, local and unannounced, tells him first, does it on his yes and checks it was done.
When the Comune is involved, it already knows what happened to every business like his and what works.
He approves; he never chases.

## Who solves each part

| Part of the problem | Status | Who |
|---|---|---|
| Taxes, IVA, F24, returns | Solved now | Commercialista; AI accountants (Fiscozen for forfettari, Fatture in Cloud); Qonto lets the commercialista pay F24s from his account |
| Payroll, hiring, contributions | Solved now | JetHR, consulente del lavoro |
| Paying | Solved now | Bank, pagoPA, app IO |
| Chamber of Commerce fees and filings | Solved now | impresa.italia.it (dashboard with deadlines, push) |
| INPS fixed contributions | Solved now | INPS cassetto (prepared F24s plus an email alert) |
| Logins without exposing passwords | Solved now | 1Password for Claude (summer 2026) |
| Specialists connected to the assistant | Very soon | Native connectors (community ones exist for Fatture in Cloud) |
| Watching his inbox, briefing, acting on his yes | Very soon | Claude Duet (21 Oct 2026), Meta Muse (8 Sep 2026, on WhatsApp); both US-only for now |
| Reading notices (PEC, IO, SEND) | Soon | The same assistants; AdE already sends deadlines to IO |
| Verified identity, business documents, deleghe | 2027–28 | IT-Wallet; European Business Wallet (COM(2025) 838, in the EU process) |
| Confirming "done" across every ente | Unlikely soon | No read APIs; AdE's API catalogue covers only bulk CU for intermediaries; the cassetto fiscale shows every F24 paid, behind his SPID |
| What the Comune di Napoli requires (TARI, CUP for dehors and sign, SUAP, accertamenti, autotutela) | Unlikely soon | Nobody: paper post, contradictory pages, dates moved by delibera, no API, market too small |
| Local rules nothing announces (food-handler and safety renewals, self-assessed levies) | Unlikely soon | Nobody: no register |
| What actually happens to businesses like his, and the leverage it gives | Unlikely ever | Nobody: each assistant sees one business, each specialist its own clients, the Comune publishes nothing |

## Where we fit

The last three rows, and only those: **the Naples layer**, one connector that assistants and specialists call.

- **Rules:** what a business like his owes in Naples, when, and where.
- **Facts and checks:** what a Naples business needs to know about itself (catasto m², TARI category,
  dehors concession, staff certificates), checked against what arrives ("this demand bills 85 m², your
  locale is 62").
- **Reality:** anonymised outcomes from every business, by municipalità: real waiting times, what the
  desk asks for, what wording works, how many others were hit by the same error.

Gennaro never sees us; his assistant just gets noticeably smarter about Naples than any other.

## What to do

**Stop building what others will do better:** the assistant, watcher, notifications and PEC sending
(Duet, Muse); the vault as a product (1Password, IT-Wallet, assistants' secure storage; keep it only as a
bridge to about 2027); per-form PDF layouts, per-user playbooks, the CLI as an interface, the file
index's cloud sources.

**Do, in order:**
1. Publish: the site 404s at its default address and the reports backend isn't deployed. Serve the
   library as a public connector.
2. Add the recurring Naples obligations as machine-checkable rules: given his facts, "you owe X on these dates."
3. Add the Naples business schema and its checks as connector tools (the useful part of the vault's schema).
4. Turn on outcomes: a connector tool to report how a pratica went, with consent; the municipalità in
   each report; incoming events (demands, inspections, unanswered PECs); "you're one of N with this."
5. Take the five procedures that matter most to L2/L3: TARI non domestica (variazione, autotutela), CUP,
   SUAP for somministrazione, dehors, insegna.
6. Distribution and funding: assistants' connector directories; Confcommercio and Confesercenti Napoli;
   local commercialisti; fund it as a public good.

**Watch:** Duet and Muse in Italy; AdE's API catalogue opening to taxpayers; IT-Wallet business
attestations; the Comune sending TARI and CUP through IO or SEND; the European Business Wallet.

## Risks

- Narrow: one city, local pratiche. Valuable to him, more public good than business.
- The outcomes half starts empty: the rules must make it useful on day one.
- If the Comune digitises well, the rules half shrinks; the outcomes half doesn't.

Detailed research behind this: `Naples micro business obligations feed.md` in this folder.
