# MVP checklist: what the ideal setup needs, and what we have

Status as of 2026-10-27, after the strategy discussion in this folder's other reports. The ideal setup:
each owner's own Claude (with Duet, once it reaches Europe) does the watching and the work; 1Password
holds the secrets; specialists handle taxes, payroll and payments; and we provide one hosted **Napoli
connector** that fills the gaps Claude has.

**Have** means it exists and works. **Partly** means it exists but not in the form the setup needs.
**Missing** means nothing exists yet. **Theirs** means another company provides it.

**1Password vs the vault.** 1Password holds only the secrets the agent must never read: the SPID login,
the CIE PIN and PUK, the PEC app password and portal passwords, payment cards. Everything else stays in
the vault (inside the Napoli connector): codice fiscale, addresses, municipalità, the business's details,
the locale and its catasto data, people and roles, permits and their expiry dates, IBAN, documents, open
pratiche. Claude has to read all of those to fill forms, and 1Password is designed so the agent can't.

## Provided by others (we only point to them)

| Needed | Status |
|---|---|
| The owner's Claude, proactive (Duet) | **Theirs**: waiting on the European launch |
| 1Password for secrets | **Theirs**: exists |
| Gmail, Calendar, Drive connectors | **Theirs**: exist |
| Fatture in Cloud, JetHR, Stripe, bank connectors | **Theirs**: Fatture in Cloud has community ones; JetHR not found yet |
| IT-Wallet, EU Business Wallet | **Theirs**: 2027–28 |

## The Napoli connector itself

| Needed | Status |
|---|---|
| A hosted, remote MCP that his Claude app can add | **Missing**: our MCP runs only locally, in Claude Code |
| One-click setup and one setup chat | **Missing** |
| Hosting per business, EU, encrypted | **Missing**: the encryption design exists in the vault |

## Profile (the vault's structured facts)

| Needed | Status |
|---|---|
| Schema: identity, business, locale, people, roles, permits, dates | **Have** |
| Italian validation (codice fiscale, partita IVA, catasto, IBAN…) | **Have** |
| Street → municipalità, quartiere, ASL district | **Have** (offline Naples stradario) |
| Clients with procura, retention, per-person keys | **Have** |
| Open pratiche and expiry tracking | **Have** |
| Documents, encrypted | **Have** |
| Fill the profile from a visura and photos in one chat | **Missing** |
| All of the above, hosted instead of on a Mac | **Missing** |

## PEC

| Needed | Status |
|---|---|
| Read PEC (list, read, attachments) | **Have**: local only; password in the Mac Keychain |
| Send PEC after his tap | **Missing**: deliberately left out so far |
| Handling Aruba's 90-day app password renewal | **Missing** |

## The Naples layer

| Needed | Status |
|---|---|
| Sourced procedures with honest claim status | **Have**: 30 procedures, all L1 |
| Situations (opening a bar, closing, a fine…) | **Have**: 7 |
| Offices by area | **Have**: 4 lists |
| Forms mapped to facts; signing rules by channel | **Have** |
| Validation and CI | **Have** |
| "What applies to me?" as rules a program can check | **Missing**: written as prose |
| Recurring dates (TARI, CUP, diritto annuale…) | **Missing** |
| Recognising notices, deadline counted from delivery | **Missing**: 4 letter procedures, no recognition data |
| Offices confirming the procedures (L2/L3) | **Missing** |
| Building-works procedures for architects | **Partly**: the CILA only |
| Immigration procedures for Accoglienza Fittizia | **Missing** |

## Doing the work

| Needed | Status |
|---|---|
| Sign gate (the file approved is the file sent) | **Have**: local only (Mac, Touch ID) |
| Exact filling of flat PDF forms | **Have**: fill.py, 4 layouts, local only |

## Outcomes

| Needed | Status |
|---|---|
| Backend with privacy rules | **Have**: not deployed |
| A tool for the assistant to report an outcome | **Missing** |
| Outcomes collected before launch | **Missing**: 0 reports |
| A stricter tier for immigration outcomes | **Missing** |

## Getting it to people

| Needed | Status |
|---|---|
| Public website | **Partly**: it builds, but isn't deployed (404) |
| Privacy page | **Partly**: exists for reports; needs to cover the connector |
| Setup guide in Italian | **Missing** |
| Partners who set people up (commercialisti, the Ordine, Accoglienza Fittizia, Confcommercio) | **Missing** |
| GDPR as processor (data-processing agreement, security review) | **Missing** |
| Funding for hosting | **Missing** |

## In short

**What we have is the knowledge and the trust engineering:** the procedures, the profile schema with
validation, the Naples street lookup, the sign gate, the PEC reader, and the outcomes backend.

**What's missing is mostly packaging and reach:** making it one hosted connector, sending PEC,
onboarding, deployment, and partners. On the content side: rules a program can check, recurring dates,
notice recognition, confirmation by offices, and the first outcomes.
