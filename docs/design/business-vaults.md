# Business and client vaults

Status: proposal (2026-10-26). Nothing here is built yet. Open decisions are marked
**Decision D1, D2, …**, each with a recommendation; they are collected at the end.

## Goal

Let a micro-business (under 5 people) keep its own data in pvault so agents can run its
pratiche, and let a professional (an architect, a geometra, a commercialista) keep their
clients' data to fill forms on the clients' behalf: a CILA on the SUE portal, a DOCFA, a
SUAP filing. Do it with the model the household work (PRs #32, #33) already introduced,
adding as few new concepts as possible.

## Where we are: the household model

A vault has one **principal**, whose IDs have no prefix (`identity.codice_fiscale`), and
other **parties** under a slug (`leo:identity.codice_fiscale`, documents in `leo:cie`).

What makes it work, and should be kept:

- **No database change.** The stored category (`leo:cie`) is the HKDF info, so each person
  gets their own subkeys for free.
- **One set of rules.** Every category-keyed rule strips the prefix with `SplitMember`
  first; `TestMemberRulesMatchOwner` checks that a party gets exactly the owner's rules.
- **A small scope grammar** (`leo:*`, `leo:cat.*`, `*:cat.*`, `*`) parsed the same way in
  Go, `mcp/src/scope.ts` and the Touch ID helper, tested against
  `internal/vault/testdata/scopes.json`.
- **Facts, not procedure rules.** `famiglia` stores the relazione (with its ANPR Tabella
  05 code), a_carico, genitori, rappresentanza. Who counts for an ISEE or a 730 is up to
  each playbook.
- **Safe removal.** `RemoveMember` runs in one transaction, refuses while another field
  still names the person, and revokes the tokens and grants whose scope names them.

### Cleanup before extending it (phase 0)

1. **The general mechanism uses family words.** `member`, `Household`, `/vault/members`,
   `ScopeAllowsWholeMember`, `MemberRows` appear in about 31 non-test files. The mechanism
   means "any other party"; `pvault member add rossi --relazione cliente` would read wrong.
   Rename the general layer (scope, store, API, CLI) and keep `famiglia` and
   `pvault member` as the family view. See **D1** for the word.
2. **"Owner-only / member-only" is written twice.** `famigliaOwnerFields` and
   `famigliaMemberFields` in `famiglia.go` repeat the `For:` tags in `schema.go`. The
   schema should be the only source; the rules read it.
3. **Fields that name a person are a hard-coded switch.** `isPersonRefField` lists
   `famiglia.genitori`, `famiglia.intestatario_scheda` and pratiche `persona`. A business
   adds many more (legale_rappresentante, committente, procuratore). Declare it on the
   schema field instead (e.g. `Refs: "person"`), and drive `checkPersonRefs` and
   `personRefsTo` from that.

## The model: two rules

1. **A vault is one titolare**: the person or business legally responsible for the data
   in it, and the unit you would ever hand to someone else. Each vault has its own password
   and secret key, backups, audit log and Touch ID enrollment.
2. **Inside any vault: one principal plus parties.** Exactly the household model: the same
   prefix, scopes, per-party keys, and "remove = erase everything about them".

```
~/.pvault          personale   principal: you          parties: family    (famiglia.*)
~/.pvault-studio   studio      principal: the impresa  parties: titolare, soci, dipendenti,
                                                        clients, impresa esecutrice (ruolo.*)
```

The only things that differ between the two vaults are what the principal is and which
relation category describes the parties.

### Why the vault boundary sits there

- **It matches the law.** Processing by a natural person "in the course of a purely
  personal or household activity" is outside GDPR (art. 2(2)(c), recital 18), so family
  data lives in a vault the regulation doesn't reach. A studio is titolare of its client
  data and has the full set of obligations. (The art. 2(2)(c) point is from our own
  knowledge of the GDPR, not checked against a source during this research.)
- **It matches the password managers.** 1Password and Bitwarden keep a personal vault
  next to, never merged with, a business account or organization with its own keys
  ([1Password](https://1password.com/blog/family-versus-business),
  [Bitwarden](https://bitwarden.com/help/what-encryption-is-used/)).
- **A business vault can be handed over** (to a socio, a successor, a new legale
  rappresentante) without the owner's family coming with it.

### When to open a business vault

When either is true:

- the business is its own legal person (SRL, SRLS, SNC, SAS, studio associato, STP), or
- you start holding other people's data for work (clients, employees).

A sole trader or forfettario with neither keeps `business.*` and `locale.*` in the
personal vault, as today: a ditta individuale is the person (same codice fiscale), and
data about yourself raises no GDPR question.

### Why clients go in the studio vault, not a vault each

Password managers for IT providers use one isolated org per client
([1Password MSP](https://www.businesswire.com/news/home/20240506919600/en),
[Bitwarden Provider Portal](https://bitwarden.com/help/getting-started-providers/)),
because each client has its own users, billing and offboarding. An architect's client has
none of those. And forms mix parties: a CILA names the committente (the client), the
tecnico (the studio's architect) and the impresa esecutrice. With all three in one vault,
one Touch ID approval covers the pratica, e.g. `rossi:*,business.*,bianchi:*`; with a
vault per client every pratica needs two servers and two approvals.

Each client is still separate where it matters: their own subkeys, their own export and
erasure (`remove`), their own lines in the audit log. **Caveat:** those subkeys come from
the same vault key, so the separation protects against mistakes and over-broad scopes, not
against someone who holds the vault key.

A vault per client would only be right for a different titolare, or at a scale we are not
designing for (**D4**).

## The business vault

### Principal (no prefix)

From the visura camerale and the procedures in pratiche-napoli:

- `business.*`: denominazione, **forma_giuridica**, codice_fiscale, partita_iva, REA,
  CCIAA, ATECO, PEC, telefono, sede legale, capitale sociale, data di costituzione / inizio
  attività, data_cessazione. Several of these (`forma_giuridica`, `telefono`,
  `data_cessazione`) are already the most-requested rows in pratiche-napoli's
  `docs/proposed-fields.md`.
- `locale.*`: the premises, as today. A second unit is an open question (**D6**).
- Employer positions: INPS matricola aziendale; INAIL codice ditta, PAT, voce di tariffa;
  CCNL.
- Deadlines, as `*_data_scadenza` fields so `pvault expiring` picks them up with no new
  code: firma digitale / CNS (3-year certificate, renewable once, in person
  ([CCIAA Salerno](https://sa.camcom.it/print/3363))), PEC subscription, last DURC (valid
  120 days). Fixed calendar dates (diritto annuale CCIAA, INAIL autoliquidazione on 16
  February, monthly F24) belong in playbooks, not fields.
- `pratiche.*`, owner-only as today. `persona` names whom the pratica is for: a client's
  slug for a professional.
- The business's own credentials (Entratel/Fisconline, SPID per uso professionale) follow
  the existing personal-tier rules.

`io` keeps meaning "the vault's principal": in a business vault, the impresa.

### People are always parties, the titolare too

This works the same way for a ditta, an SRL, a studio associato and a solo professional:
the principal is the business; every person is a party. A solo professional has exactly
one titolare party.

Professional details belong to the person, not the business: `bianchi:albo.ordine`,
`albo.provincia`, `albo.numero`, `albo.data_iscrizione`, and the cassa
(`bianchi:inarcassa.matricola`). In a studio associato each architect has their own albo
entry, so these can't sit on the business.

A client can be a company: `edilnapoli-srl:business.*` already validates, because
`business` is not an owner-only category.

### `ruolo`: the business counterpart of `famiglia`

A party-only category, validated like `famiglia`:

| Field | Meaning |
|---|---|
| `ruolo.tipo` | One or more of titolare, legale_rappresentante, socio, amministratore, dipendente, collaboratore, preposto, responsabile_tecnico, procuratore, cliente, impresa. A list, like `famiglia.genitori`, since one person can hold several. |
| `ruolo.data_inizio`, `ruolo.data_fine` | When the relationship started and ended. `data_fine` starts the retention clock (see GDPR). |
| `ruolo.quota_percentuale` | A socio's share. |
| `ruolo.poteri` | Powers of an amministratore or procuratore, as on the visura. |

Employee data that UniLav and the LUL need (titolo di studio, contract type, hours,
mansione, CCNL level, place of work) can go in an `impiego` category on the party later;
it is not needed to start.

Fields that name people (`business.legale_rappresentante`, pratiche `persona`, a
procedure's committente) use the schema flag from phase 0 and must name `io` or an
existing party.

**D2:** `famiglia` and `ruolo` as two categories, or one relation category with a
vocabulary per vault kind. Recommendation: two. The vocabularies are legally distinct
(ANPR kinship codes vs company roles) and both already have clear homes.

**D3:** is a vault kind (`persona` / `impresa`, stored in `vault_meta`, set by
`pvault init --impresa`) worth adding? Recommendation: yes. It is one meta key, and it
decides the schema and onboarding page shown, refuses unprefixed `identity.*` and the
housing categories in an impresa vault, and enables the credential rule below.

## Acting on a client's behalf

**Never store a client's SPID or CIE credentials.** SPID is strictly personal under the
identity providers' terms
([InfoCert](https://www.infocert.it/pdf/identitadigitale/istruzioni_uso_identita_digitale.pdf));
the AgID rule itself is probably in DPCM 24 ottobre 2014 (not verified). In an impresa
vault, `IsCredentialField` on any party's field should be refused outright, not stored at
the personal tier. (In the personal vault a member's credentials stay allowed at the
personal tier, as today.)

The legitimate mechanisms, and what the vault should keep for each:

| Ente | Mechanism | Keep |
|---|---|---|
| SUE / SUAP | The professional files with their own SPID/CIE/CNS and digital signature and attaches the client's signed procura speciale ([Unioncamere Veneto](https://www.unioncamereveneto.it/wp-content/uploads/2021/10/20211006-Suap-Veneto-5.pdf)) | Signed procura, copy of the client's ID (usually art. 38 c.3 DPR 445/2000; varies by comune), scope, dates |
| Agenzia Entrate | "Delega unica" (art. 21 D.Lgs. 1/2024, from 8 Dec 2025): at most 2 intermediari, valid until 31 Dec of the 4th year after it is given ([ecnews](https://www.ecnews.it/fiscale/in-pratica/guida-agli-adempimenti/la-delega-unica-in-vigore-dall8-dicembre-2025/)) | Delega, activation receipt, expiry |
| INPS | Intermediari under L. 12/1979 via "Gestione deleghe indirette" | Delega, expiry |
| ComUnica | Napoli no longer requires the scanned procura; an obligated person signs digitally as dichiarante ([na.camcom](https://na.camcom.gov.it/old_site/vecchiosito.na.camcom.gov.it/attachments/article/1427/Comunicato%20eliminazione%20procura%20comunica%20-signed.pdf)) | Nothing extra |
| CAD art. 64-ter Sistema di Gestione Deleghe | Reported to launch 15 Oct 2026, possibly excluding professionals (secondary sources, not verified) | Revisit once confirmed |

In the vault: the signed documents go in the party's `procura` documents category
(`rossi:procura`), and each delega gets `rossi:procura.ente`, `procura.oggetto`,
`procura.data_conferimento`, `procura.data_scadenza`. The expiry then shows in
`pvault expiring` like any other.

## GDPR for client data

Most obligations map onto what already exists:

| Obligation | In pvault |
|---|---|
| Erasure (art. 17) | `RemoveMember`, already transactional and revoking tokens |
| Accountability (art. 5(2)) | The audit log |
| Minimisation | Scoping each pratica to `rossi:*` instead of `*` |
| Security (art. 32) | Encryption at rest, per-party subkeys, Touch ID approvals, encrypted backups |

What's missing:

- **Portability (art. 20):** a per-party export (`pvault person export rossi`: fields and
  documents of one party).
- **Retention:** about 10 years after the job ends is the defensible period for an
  architect's pratica file (art. 2946 c.c. contractual liability, art. 1669 for serious
  defects). That figure is from the research agent's own knowledge and **needs checking**
  before it goes into code. ID copies and procure should go much sooner. Proposal:
  `pvault expiring` reports a party whose `ruolo.data_fine` is older than the retention
  period, and a party's ID scans once their pratiche are closed.
- **Registro dei trattamenti:** the under-250-employee exemption doesn't cover regular
  processing of client files, and the Garante names professionals with at least one
  employee ([Assolombarda on the Garante FAQ](https://www.assolombarda.it/servizi/privacy/gdpr-le-istruzioni-del-garante-privacy-sul-registro-dei-trattamenti)).
  Out of scope for the vault itself; a playbook can produce one.
- **Cloud AI:** an agent model that reads client data makes its provider a responsabile
  del trattamento under art. 28. The studio needs a business tier with a DPA and no
  training on the data, not a consumer plan. A note in the vault skill and the onboarding
  page, not code.

## Running several vaults

Each vault stays exactly today's product: its own dir, server, socket and port. Changes:

- **Named vaults.** `pvault --vault studio` (or `PVAULT_VAULT=studio`) resolves the dir
  and port; the default stays `~/.pvault` on 7200. Sibling dirs (`~/.pvault-studio`), not
  nested ones, so backup and restore checks about "outside the vault dir" keep working
  (**D5**).
- **Touch ID.** The Keychain item is already keyed per vault dir. The helper hard-codes
  `http://127.0.0.1:7200` (`cmd/pvault-touchid/main.swift:43`), and pvault skips Touch ID
  when `VAULT_ADDR` differs; store the vault's address in the enrollment JSON next to
  `server` instead.
- **MCP: no code change.** Register one instance per vault (`vault`, `studio`), each with
  its own `VAULT_ADDR` and token; the tools are namespaced by server name automatically.
  The vault and pratica skills learn which instance holds what.
- **Not one server hosting many vaults.** It would multiply lock state, grants and
  auto-lock inside one process; the process boundary between vaults is a feature.
- **Accepted duplication.** A solo professional's identity sits in both vaults (as
  principal in one, titolare party in the other). A one-shot copy
  (`pvault person import --from personale io bianchi --categories identity,cie,residenza`)
  is simpler than links across vaults, and keeps the studio vault able to stand alone.
- **Moving a sole trader's business out** of the personal vault when they open a studio
  vault: a one-off `business.*`/`locale.*` move command.

## Payoff for pratiche-napoli

Seven rows of `docs/proposed-fields.md` there are "another person": `procuratore.*`,
`tecnico.*`, `responsabile_tecnico.*`, `business.soci<N>_*`, `business.preposto_*`,
`subentrante.*` and the previous holder. All become a party with a `ruolo`, and procedures
refer to people by role ("the committente is a party with ruolo cliente") instead of
inventing a category each time. The official forms already work this way: Agenzia Entrate
returns separate the persona fisica or società from the rappresentante firmatario, and
DOCFA separates the dichiarante, the intestatari catastali and the professionista
redattore.

`locazione.locatore_*` in the personal vault is the one existing "other person" stored as
owner fields. It can stay; migrating it to a party is optional.

## Phases

0. **Cleanup:** the general naming (D1); `For:` and person-reference flags in the schema
   as the only source.
1. **Business vaults:** vault kind (D3), `--vault`, the Touch ID address, the business
   principal schema, `ruolo`, refusing other parties' credentials in impresa vaults.
2. **Clients:** `--ruolo cliente`, procura documents and expiry, per-party export, the
   retention report.
3. **Library:** replace those proposed fields with party references in the
   pratiche-napoli contract (`contract/extensions.json`, `fields.json`).

## Open decisions

- **D1. The general word for a party.** Options: member, person, party, subject.
  Recommendation: **person** in the CLI and API (`pvault person add rossi --ruolo
  cliente`), since persona fisica / giuridica covers companies too; keep
  `pvault member` as the family alias. The internal rename can follow mechanically.
- **D2. `famiglia` and `ruolo`: two categories or one.** Recommendation: two.
- **D3. A vault kind (`persona` / `impresa`).** Recommendation: yes, one meta key set at
  init.
- **D4. Clients in the studio vault or a vault each.** Recommendation: in the studio vault;
  revisit only for a different titolare or much larger scale.
- **D5. Where named vaults live.** Recommendation: sibling dirs (`~/.pvault-<name>`), each
  with its own port.
- **D6. More than one premises unit** (`locale2.*`, `locale.immobile<N>_*`, or units as
  something new). Not decided; pratiche-napoli's TARI non domestica procedures need it.
- **D7. Several users** (staff sharing a studio vault). Recommendation: out of scope for
  businesses under 5 people, where the titolare runs the agents; it would be a different
  product (sync, per-user keys).
- **D8. Retention periods.** The 10-year figure for architects and the timing for deleting
  ID copies need checking against sources before any code reports on them.
