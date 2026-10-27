# Three avatars: what's covered, what's coming, where the real gap is

Synthesis of the 2026-10-27 strategy discussion across three personas in the Comune di Napoli: Gennaro the
pizzeria owner, Chiara the architect, and Accoglienza Fittizia, the impresa sociale that does permesso di soggiorno
paperwork for migrants. The detail and sources are in the three reports in this folder:
`Gennaro strategy conclusion.md`, `Architect strategy conclusion.md`; the report on the third persona is not published.

**Covered** means handled today. **1–2 yrs** means very likely handled by 2028. **Gap** means nobody is on
track to fill it.

## Coverage, layer by layer

| Layer | Gennaro (pizzeria) | Chiara (architect) | Accoglienza Fittizia (migrant services) |
|---|---|---|---|
| **Own taxes, accounts, payroll** | Covered: commercialista, AI accountants, JetHR | Covered: commercialista; Inarcassa online | Covered: commercialista. The impresa sociale rules are niche but routine |
| **Paying** | Covered | Covered | Covered: Poste, bollettini |
| **Logins and signing** | Covered: 1Password for Claude, firma digitale | Covered | Covered for Accoglienza Fittizia itself; clients often have no SPID |
| **Filing channels** | Covered: impresainungiorno (SUAP), PEC | Covered: impresainungiorno (SUE since July 2026), SISMICA | Covered: Poste kit, ALI portal |
| **Explaining, translating, drafting** | Covered: Claude | Covered: Claude | Covered: Claude, Sportellino (free, on WhatsApp, in the clients' languages) |
| **Specialist software for the core work** | Not needed | Covered: CAD, BIM, ACCA, Blumatica | Unclear: patronati have their own systems; nothing found for a one-person organisation |
| **Watching the inbox, briefing, acting on a yes** | 1–2 yrs: Claude Duet, Meta Muse (US-only today) | 1–2 yrs | 1–2 yrs for Accoglienza Fittizia; its clients mostly won't use these assistants |
| **Verified identity, documents, mandates** | 1–2 yrs or a bit more: IT-Wallet, EU Business Wallet (2027–28) | Same | Partly: Italian documents yes; foreign documents and apostilles no |
| **Tracking status across many pratiche** | Partly: cassetto fiscale behind SPID, no APIs | Partly: impresainungiorno tracks its own; Soprintendenza and Genio Civile don't | Gap: one client at a time, on two portals, with each client's own password |
| **Written local rules** | Shrinking gap: better models read sources, but Naples pages contradict each other (TARI and CUP dates) | Shrinking gap: rules move (Salva Casa, the 2025 solar decree) | Gap: pages missing entirely (no official Naples page on idoneità alloggiativa) |
| **Unwritten local practice** | Gap: SUAP, TARI office, Napoli Obiettivo Valore | Gap: Soprintendenza, SUE, the building archive, Genio Civile | Gap: Questura, Prefettura, anagrafe, ASL; offices bending the law |
| **Collective evidence and leverage** | Gap: identical wrong TARI demands | Gap: Soprintendenza patterns | Gap: illegitimate demands at renewal, for advocacy groups |
| **Obligations nothing announces** | Gap: self-assessed TARI and CUP, certificates, lapsing deleghe | Covered: Inarcassa and CFP are routine | Gap: permit expiries across every client |
| **Holding third parties' sensitive data** | Not applicable: employees go through JetHR | Partly: clients' deeds and procure; moderate sensitivity | Gap: special-category data on hundreds of vulnerable people |

**The overall shape:** everything national, standard and money-related is covered or close. What's left is
local, unwritten, collective, and, for intermediaries, many clients at once.

## Is there real crossover?

**On content: not much.** The offices barely overlap:
- Gennaro: Comune tributi, SUAP, ASL food.
- Chiara: SUE, Soprintendenza, Genio Civile, the building archive.
- Accoglienza Fittizia: Questura, Prefettura, anagrafe, ASL registration.

A Questura waiting time is worthless to Gennaro, and a Soprintendenza pattern is worthless to Accoglienza Fittizia.

**On structure: deep.** All three have the same kind of gap, and one engine fills it:
- one library format (sourced claims, verification levels);
- one outcomes backend with one privacy model (minimum group sizes, no traceable detail);
- one connector that assistants call.

Build it once, then fill it per segment.

**At a few real handoff points:**
1. **Opening a business.** Gennaro's SUAP, dehors and sign often go through an architect or geometra.
   Accoglienza Fittizia's start-up clients become Gennaros: migrant-run shops around Piazza Garibaldi. One chain
   (ComUnica → SUAP → SUE → TARI) touches all three.
2. **Idoneità alloggiativa.** A migrant's housing certificate usually needs a technician's inspection: an
   architect or geometra. That links Accoglienza Fittizia and Chiara, and nobody has written the Naples version.
3. **The same platform.** Since July 2026, Gennaro's SUAP and Chiara's SUE run on impresainungiorno, so
   outcomes about the platform itself help both.

**In roles: complementary, not overlapping.** Chiara and Accoglienza Fittizia are intermediaries: they file dozens of
pratiche a year, so they contribute outcomes. Gennaro files one every few years, so he mostly benefits.
The network works because those roles differ, not because the needs overlap.

## What this means

- **One engine, three segments.** Don't build three products. Build the Naples layer once, and fill it
  segment by segment.
- **Start where the segments meet:** the business-opening chain (ComUnica, SUAP, CILA/SUE, dehors,
  insegna, TARI non domestica). It serves all three, architects can supply the outcomes, and most of it is
  already in the library.
- **Then split.** Building works for architects, immigration for Accoglienza Fittizia. Each has its own content, and
  immigration has a stricter privacy tier.
- **The vault's structured profile matters to all three,** inside the one hosted Napoli connector;
  1Password holds only the secrets, because it never lets the agent read anything (corrected later the
  same day; see `MVP checklist.md` and `Vault sensitivity ranking.md`). Accoglienza Fittizia's many-client data is its
  heaviest, highest-risk use, so that part comes later, with stricter handling.

## How sure is this?

- **Firm:** the national, standard layers are covered, and the local unwritten layer is a gap for all three.
- **Less firm:**
  - when Duet and Muse arrive in Italy;
  - the IT-Wallet and EU Business Wallet timelines;
  - whether specialist software exists for small immigration services (not researched);
  - Accoglienza Fittizia's own data, which is inferred (their website blocks automated tools).
