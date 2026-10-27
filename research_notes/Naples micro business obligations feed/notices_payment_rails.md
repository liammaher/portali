# Inbound-notice and payment rails for a Naples ditta individuale (pagoPA, IO, SEND, AdER, PEC, PDND, SPID) — and whether a tool acting for the owner can read them

Persona: Gennaro, pizzeria in Naples, ditta individuale (his codice fiscale is a natural person's CF; separate partita IVA), PEC on INI-PEC, SPID, app IO, commercialista. Research date 2026-10-27. Budget-limited pass (about 17 tool calls); several points are marked unverified. Sender-side (ente) access is kept separate from recipient-side access throughout.

## 1. pagoPA: avvisi, IUV/codice avviso, whether a payer can list pending avvisi, ricevute telematiche

### Takeaway
pagoPA has no recipient-side API or public "list all my pending avvisi" service. A payer sees an avviso only when a creditor pushes it to him: on paper or PDF, as an IO message, or attached to a SEND notification. He sees receipts in IO's payments area (reportedly including payments made outside IO), and the PSP also sends receipts by email. For a tool, the readable sources are the avviso documents themselves (PDF/QR in PEC, SEND attachments) and receipt emails, not a pagoPA API.

### Cited Findings
- The avviso's technical spec defines a recipient field `DESTINATARIO.CF` as "Codice Fiscale o Partita IVA del debitore a cui è intestata la posizione debitoria". So an avviso can be addressed to a P.IVA as well as a CF, and the field may be omitted only when unknown. — [docs.pagopa.it, Avviso di pagamento, Informazioni sul Destinatario](https://docs.pagopa.it/avviso-pagamento/allegato-2/specifiche-tecniche/informazioni-sul-destinatario)
- SEND notifications can carry payment attachments of type `PAGOPA` (the avviso) or `F24`. The SEND recipient API exposes them as `/delivery/notifications/received/{iun}/attachments/payment/{attachmentName}` with `attachmentName` = `PAGOPA` | `F24`. — [pagopa/pn-delivery, pn-delivery-b2b-pg-external.yaml](https://raw.githubusercontent.com/pagopa/pn-delivery/main/docs/openapi/pn-delivery-b2b-pg-external.yaml)
- A ricevuta telematica (RT) reaches the user within 24 hours of payment. For a formal quietanza, the guide refers the payer to the creditor ente named on the avviso. — [Fastweb guide (secondary)](https://www.fastweb.it/fastweb-plus/digital-magazine/come-scaricare-ricevuta-pagamento-pagopa/)
- When paying at a bank, post office or tabaccheria, the receipt comes from that channel. In IO, the success message is stored in the Pagamenti section. — [Fastweb (secondary)](https://www.fastweb.it/fastweb-plus/digital-magazine/come-usare-lapp-io-per-ricevere-e-pagare-avvisi-di-tasse-e-tributi/)
- The IO store listing says IO lets users download pagoPA receipts "anche se non hai pagato tramite l'app". — [mwm.ai mirror of IO App Store listing (secondary)](https://mwm.ai/apps/io/1501681835)
- The PSP emails the receipt after payment is confirmed. — low-quality source: [tattoomuse](https://tattoomuse.it/wiki/come-pagare-pagopa-con-app-io)
- 2026 payment-method additions in IO, all for paying avvisi pagoPA:
  - Google Pay on Android (v3.24.1.0, Feb 2026). — [TuttoAndroid 2026-02-19](https://www.tuttoandroid.net/news/2026/02/19/app-io-aggiornamento-pagamento-avvisi-pagopa-con-google-pay-1140607/)
  - Fabrick Pay by Bank (iOS 3.33.0 from 9 June 2026; an Android article dates it to 27 Aug 2026, v3.37.1.0, so the two dates conflict). — [MondoMobileWeb](https://www.mondomobileweb.it/321762-app-io-nuovo-aggiornamento-aggiunto-fabrick-pay-by-bank-per-pagare-avvisi-pagopa/); [Webnews](https://www.webnews.it/app-io-pagopa-pagamento-conto-corrente-android/)

### Inferences
- Pending debt is held per creditor. pagoPA's debt-position registry (GPD) and the creditor's own systems are sender-side, and I found no recipient-facing "my pending avvisi" listing. The tool therefore has to collect avvisi where they land: PEC attachments, SEND notifications, IO messages, paper scans. It can parse the codice avviso (18 digits) and the ente CF from the PDF or QR to dedupe and track them.
- Each avviso is identified by (CF ente creditore, codice avviso / IUV), so the tool can check "paid?" by matching an RT or receipt email against that pair. Receipts emailed by the PSP to Gennaro's ordinary mailbox, and IO payment messages, are the practical "done" signal.
- An avviso can be addressed to the P.IVA rather than the CF. Avvisi under the P.IVA may therefore not appear in IO, which is keyed to his personal CF (see section 2).

### Gaps
- I could not open the IO help pages to confirm whether IO's "Ricevute pagoPA" covers every RT where his CF is debtor or payer, or only some. This is unverified.
- Not confirmed: any official pagoPA citizen-facing portal that lists pending avvisi across creditors. I found none, and I believe none exists, but no source states that.
- Not verified this session: the GPD (Gestione Posizioni Debitorie) and Biz-Events services are sender- or PagoPA-internal, with no third-party payer access.

## 2. App IO: who receives what, a ditta's partita IVA, reminders, payment, recipient-side API, 2026 status

### Takeaway
IO is an app for natural persons, logged in with SPID or CIE and keyed to the personal CF. A ditta individuale shares its owner's CF, so messages and SEND notifications addressed to Gennaro's CF reach his IO. Anything addressed to the P.IVA alone would not, which is an inference. IO has a sender-side Services API for enti only. I found no recipient-side API or export: a tool cannot read Gennaro's IO inbox. Its value to the tool is indirect, through the push and courtesy emails it triggers and through SEND, which has its own recipient channels.

### Cited Findings
- Sole traders whose CF corresponds to the natural person ("imprese individuali (in cui il CF dell'intestatario corrisponde alla persona fisica)") receive, read and manage SEND notifications like citizens. They use SPID or CIE and channels such as the IO app, email, SMS or PEC. Other companies follow the separate business route. — [Agenda Digitale, SEND per cittadini e imprese](https://www.agendadigitale.eu/cittadinanza-digitale/send-le-notifiche-digitali-della-pa-per-cittadini-e-imprese-perche-e-utile/)
- A notification on IO has legal value only if it is sent through SEND and read within the set terms. Fiscal communications with legal value require SEND to be active. — [Studio Cavallari, Notifiche fiscali su App IO 2026](https://www.studiocavallari.it/agevolazioni-e-novita-fiscali/notifiche-fiscali-su-app-io-2026-valore-legale-rischi-e-cosa-deve-fare-il-contribuente)
- Recent iOS versions can scan the QR codes on paper notices of a SEND notification. — [search summary of the 2026 IO news set](https://www.scuolainforma.news/app-io-il-super-aggiornamento-di-giugno-2026-ecco-tutte-le-nuove-funzioni/) (secondary)
- The Dipartimento della Giustizia Tributaria has sent real-time notifications on IO since 3 June 2026. — [same 2026 IO coverage (secondary)](https://www.businesspeople.it/news/il-fisco-arriva-sullo-smartphone-tutte-le-novita-sullapp-io-nel-2026/)
- In June 2026 Agenzia delle Entrate-Riscossione was working to connect its services to IO so taxpayers get collection notices on the phone. The article stresses the difference between informational messages and legally valid notifications, which would go through SEND. — [Fiscal Focus, 11 June 2026](https://www.fiscal-focus.it/news,185491)
- The pn-delivery repo contains `api-external-b2b-appio.yaml` and `schemas-pn-timeline-appio.yaml`, the SEND↔IO integration. These are platform-to-platform APIs (SEND feeding IO), not a user API. — [github.com/pagopa/pn-delivery docs/openapi](https://github.com/pagopa/pn-delivery/tree/main/docs/openapi)
- Latest IO versions seen: Google Play update dated 7 Oct 2026, and 3.40.0.14 dated 15 Oct 2026 on Uptodown. — [Google Play](https://play.google.com/store/apps/details?id=it.pagopa.io.app&hl=en_US); [Uptodown](https://io.en.uptodown.com/android)

### Inferences
- **No legitimate programmatic read of IO for the recipient.** The IO "Services API" (api.io.pagopa.it) lets an onboarded ente send messages to a CF. It is sender-side, and a private tool cannot become an IO sender for reading. The ways left are:
  - the user forwards or screenshots IO messages;
  - IO's own email forwarding of messages to his verified email (a setting in IO; not verified this session), which the tool could then read in his ordinary mailbox with his consent.
- Scraping IO through an emulated app session would mean logging in with his SPID/CIE, which runs into section 7.
- IO "scadenze" reminders work as an "add to calendar" button on messages that carry a due date (from my background knowledge; not verified this session). An ICS export is therefore at best per message and manual.

### Gaps
- Not verified this session: an io.italia.it statement that IO is only for persone fisiche, and the current IO email-forwarding ("inoltro messaggi via email") option. Background knowledge says both exist; check io.italia.it/faq.
- Not verified: whether IO shows avvisi addressed to a P.IVA. Probably not, since IO is keyed to the CF.
- The status of IT-Wallet and "SEND in IO" details in 2026 beyond the press items above.

## 3. SEND (Servizio Notifiche Digitali): delivery to a business, perfection timing, portal, recipient APIs, deleghe, enti using it

### Takeaway
SEND delivers to the recipient's domicilio digitale, which for Gennaro is the PEC on INI-PEC. It also sends courtesy messages through IO, email and SMS. A notification is perfected 7 days after PEC delivery, or earlier if he opens it on SEND or IO, and reading is not required. A ditta individuale uses the citizens' portal (cittadini.notifichedigitali.it) with SPID/CIE. The legal-entity route (imprese.notifichedigitali.it, B2B API for "destinatari strutturati") is for persone giuridiche, and its recipient B2B API is not published on the developer portal. For a tool, the realistic routes are:
- read the SEND PEC messages in his mailbox with his consent;
- have him grant a SEND delega to a delegate.

### Cited Findings
- **Perfection timing:**
  - For recipients with an active domicilio digitale (SEND or PEC), a notification is perfected 7 days after delivery. If delivered after 21:00, the count starts the next day.
  - If the PEC is full, invalid or inactive, it is perfected 15 days after the notice of failed delivery is deposited, and that notice is visible in the SEND notification details.
  - Opening the documents on SEND or IO earlier perfects the notification at the moment of access.
  - [notifichedigitali.it/perfezionamento](https://notifichedigitali.it/perfezionamento); [SEND assistance, Quando si perfeziona una notifica](https://assistenza.notifichedigitali.it/hc/it/articles/32452602728977-Quando-si-perfeziona-una-notifica) (as summarised in search results; pages not opened in full)
- For PEC-notified tax acts, the recipient-side effect is tied to the delivery receipt (ricevuta di avvenuta consegna). Not reading the PEC does not stop the notification from taking effect. — [tributiconsulting (secondary)](https://tributiconsulting.it/cartelle-esattoriali/notifica/pec)
- Documents remain available for up to 120 days. — [Agenda Digitale (secondary)](https://www.agendadigitale.eu/cittadinanza-digitale/send-le-notifiche-digitali-della-pa-per-cittadini-e-imprese-perche-e-utile/)
- **Portals:** cittadini.notifichedigitali.it for citizens. imprese.notifichedigitali.it for legal representatives of a company or those with permission to manage company notifications. Sole traders whose CF is the person's follow the citizen route. — [Agenda Digitale](https://www.agendadigitale.eu/cittadinanza-digitale/send-le-notifiche-digitali-della-pa-per-cittadini-e-imprese-perche-e-utile/)
- **Persone giuridiche:** the official manual says they are recipients "che non possono essere configurati come persone fisiche". It also says "La persona giuridica non accede a PN direttamente ma tramite una persona fisica", the legale rappresentante, who then enables other users. — [developer.pagopa.it, Manuale operativo, Destinatari, Persone giuridiche](https://developer.pagopa.it/send/guides/manuale-operativo/destinatari/persone-giuridiche)
- On first access to the business portal, the legale rappresentante must appear in public registers: Registro Imprese, or failing that Anagrafe Tributaria. Per the search summary of SEND help pages, liberi professionisti and ditte individuali are excluded from this route. — search summary citing [developer.pagopa.it FAQ](https://developer.pagopa.it/send/guides/knowledge-base/readme/inserimento-della-notifica/faq-inserimento-notifiche) (the exact sentence was not seen in a fetched page; treat as likely but unverified)
- **Published APIs:** developer.pagopa.it/send/api lists only two:
  - "API B2B per le PA", to send notifications and read their status;
  - an "API di status" for platform availability.
  - No recipient API is listed there. — [developer.pagopa.it/send/api](https://developer.pagopa.it/send/api)
- **Recipient B2B API in source code:** pagopa/pn-delivery contains `pn-delivery-b2b-pg-external.yaml`, titled "Piattaforma Notifiche: API B2B per destintari strutturati" and described as "API utilizzate dalle persone giuridiche per avere la lista delle notifiche e il dettaglio di ognuna di queste". Its operations:
  - `GET /delivery/notifications/received` (search by date range, sender, IUN, mandate ID);
  - `GET /delivery/notifications/received/delegated` (notifications received through a delega, filterable by recipient and group);
  - `GET /delivery/v2.8/notifications/received/{iun}` (detail with timeline, status history, documents, payment info);
  - document and payment-attachment (PAGOPA/F24) downloads.
  - Its bearer-JWT security block is marked "ONLY EXTERNAL" and commented out; no API-key or PDND-voucher mechanism is described. — [pn-delivery-b2b-pg-external.yaml](https://raw.githubusercontent.com/pagopa/pn-delivery/main/docs/openapi/pn-delivery-b2b-pg-external.yaml)
- The same repo has `api-internal-web-recipient.yaml`, the API behind the recipient web portal, which is internal. — [pn-delivery docs/openapi](https://github.com/pagopa/pn-delivery/tree/main/docs/openapi)
- A PR titled "PN-18958 Update api b2b PG" is in pn-delivery, so the PG API is under active development. — [pn-delivery pulls](https://github.com/pagopa/pn-delivery/pulls)
- **Deleghe:** a recipient can delegate the management or reading of his notifications to other persone fisiche or giuridiche through the Deleghe section. Both delegante and delegato need SPID. — [Agenda Digitale](https://www.agendadigitale.eu/cittadinanza-digitale/send-le-notifiche-digitali-della-pa-per-cittadini-e-imprese-perche-e-utile/); [Punto Informatico](https://www.punto-informatico.it/send-servizio-notifiche-digitali-pec-inad/)
- **Sender side:** enti send through API B2B (upload attachments with SHA-256, then request the notification and get the IUN) and follow status through a stream. The x-api-key is generated in the PA back office, optionally with a PDND voucher. — [docs.pagopa.it, Invio notifiche tramite API B2B](https://docs.pagopa.it/manuale-operativo/piattaforma-notifiche-digitali-manuale-operativo/mittente/referente-tecnico-operatore/utilizzo-di-send-attraverso-api-b2b/invio-notifiche-tramite-api-b2b)
- **Enti in 2026:**
  - The Ministero della Giustizia joined SEND (June 2026). — [Altalex, 11 June 2026](https://www.altalex.com/documents/news/2026/06/11/notifiche-digitali-ministero-giustizia-aderisce-piattaforma-send)
  - Roma Capitale has a SEND service notice. — [comune.roma.it](https://www.comune.roma.it/web/it/informazione-di-servizio.page?contentId=IDS1471360)
  - AdER: see section 4.

### Inferences
- **The PEC is the one feed that carries everything SEND delivers.** Because Gennaro has a PEC on INI-PEC (the domicilio digitale register for imprese and professionisti), SEND delivers there and the 7-day clock starts on PEC delivery. A tool reading his PEC by IMAP sees each SEND "avviso di avvenuta ricezione" (with IUN and sender) on the day it lands. That is exactly when the legal clock starts.
- **Reading the PEC does not perfect the notification.** Only access to the documents on SEND/IO perfects early. IMAP reading of the PEC message is not access on SEND (inference from the rule's wording). So a tool reading the PEC does not shorten Gennaro's terms, but it must not open the SEND documents through his session either.
- **A delega is the legitimate route to full documents.** A SEND delega from Gennaro to a delegate (his commercialista, or a company running the tool, if a PG) is designed for third-party reading. The delegated-search endpoint suggests a PG delegate could later read delegated notifications by API. Two blockers: that API is not published for external use, and a PG delegate today reads in the imprese portal through a person's SPID login.
- **The PG recipient API likely doesn't cover him.** A ditta individuale is not a PG for SEND, so the PG B2B recipient API would not cover Gennaro's notifications as recipient even when it becomes available.

### Gaps
- Not confirmed: whether the B2B PG recipient API is in production for any external consumer in 2026, how it authenticates, and how a PG obtains credentials. It is absent from developer.pagopa.it.
- Not opened: the official SEND help-center page on deleghe (limits, acceptance code, whether a PG delegate can be an arbitrary private company).
- Not found: whether Comune di Napoli sends multe or TARI through SEND in 2026. I found no Napoli-specific source; check comune.napoli.it and notifichedigitali.it's enti list.
- The exact legal basis for 7-day perfection (art. 26 D.L. 76/2020) was not re-read this session.

## 4. Agenzia delle Entrate-Riscossione (AdER): estratto conto, cartelle, rate and rottamazione, Equiclick, "Se mi scordo", intermediaries, API

### Takeaway
AdER shows the debt position (estratto conto/situazione debitoria), cartelle, rateizzazioni and rottamazione plans in its area riservata with SPID/CIE/CNS. Intermediaries such as the commercialista reach the same data through EquiPro under the delega unica (max two intermediaries, valid to 31 Dec of the fourth following year). "Se mi scordo" sends SMS/email reminders before installments. I found no public API for taxpayers or third parties. A tool can read AdER only through:
- reminder emails or SMS;
- PEC cartelle;
- documents the user or commercialista downloads.

### Cited Findings
- **Delega unica:** a single delegation covers Agenzia Entrate and AdER online services. The taxpayer gives it online with SPID/CIE/CNS, naming the intermediary and the services, or the intermediary submits it digitally. — [Agenzia Entrate, La delega unica](https://www.agenziaentrate.gov.it/portale/documents/d/guest/la_delega_unica_per_l-utilizzo_dei_servizi_online); [Comunicato 9 Dec 2025](https://www.agenziaentrate.gov.it/portale/-/comunicato-stampa-del-9-dicembre-2025)
- New delegations last until 31 December of the fourth year after the grant. Delegations active on 5 Dec 2025 stay valid to their expiry, but no later than 28 Feb 2027. A taxpayer can delegate at most two intermediaries. — [IPSOA guide](https://www.ipsoa.it/guide/funziona-delega-unica-servizi-online-agenzie-fiscali); [EC News](https://www.ecnews.it/lavoro/news-del-giorno/delega-unica-intermediari-servizi-agenzia-entrate-agenzia-entrate-riscossione/)
- Under the delega, intermediaries can see the debt position, request rateizzazioni, file sospensione legale requests, manage definizione agevolata (rottamazione) applications and book video appointments, all through EquiPro. — [search summary of delega unica sources](https://www.ecnews.it/lavoro/news-del-giorno/delega-unica-intermediari-servizi-agenzia-entrate-agenzia-entrate-riscossione/); [AdER, Come delegare un intermediario a EquiPro (Area Imprese)](https://www.agenziaentrateriscossione.gov.it/export/sites/ader/.files/it/associazioni/Come-delegare-un-intermediario-a-EquiPro-AREA-IMPRESE.pdf)
- **"Se mi scordo":** a free service sending SMS or email before each installment's due date, activated in the area riservata. Historically it also warned of irregular payments, imminent loss of the plan and upcoming avvisi. — [Money.it](https://www.money.it/rottamazione-4-esiste-se-mi-scordo); [Studio Cataldi (older, 265k sign-ups)](https://www.studiocataldi.it/articoli/31542-fisco-265mila-iscrizioni-a-quotse-mi-scordo-quot.asp). These are press items; I found no official 2026 page confirming that it covers rottamazione-quinquies plans.
- **Calendar service:** AdER launched a service on 30 March 2026 showing installment dates for 2026–2028. Rottamazione-quinquies and riammessi 2026 installments fall on 31 May, 31 Jul, 31 Aug, 31 Oct and 30 Nov. One headline says quinquies has no tolerance days, unlike quater's 5 days. — [diritto.it](https://www.diritto.it/rottamazione-quater-quinquies-scadenze-ader-2026-27/)
- **AdER and IO/SEND:** in June 2026 AdER was still working to integrate with IO, with legally valid notices expected via SEND. — [Fiscal Focus, 11 June 2026](https://www.fiscal-focus.it/news,185491)
- AdER suspended notifications in August 2026 except urgent cases (announcement of 6 July 2026). — [Idealista](https://www.idealista.it/news/node/413012); [Edotto](https://edotto.com/articolo/cartelle-esattoriali-stop-alle-notifiche-ad-agosto-2026)
- New collection code (D.Lgs. 33/2025, art. 211): debts assigned from 1 Jan 2025 and not collected within five years are automatically written off on 31 Dec of the fifth year. — [Idealista](https://www.idealista.it/news/node/413012)

### Inferences
- For Gennaro, cartelle reach his PEC, since companies and professionals receive them at their INI-PEC address. "Se mi scordo" emails and SMS give a second, machine-readable signal for installments. The commercialista sees the full position via EquiPro.
- A free tool cannot become an EquiPro intermediary: that role is reserved to qualified intermediaries (commercialisti, CAF, and so on). The legitimate data paths are:
  - parsing PEC cartelle and "Se mi scordo" emails;
  - the user uploading the estratto conto PDF;
  - an export the commercialista shares.

### Gaps
- Equiclick (AdER's app) features in 2026 were not researched this session (budget). Background knowledge: it shows the situazione debitoria and lets you pay. No export or API was confirmed.
- No AdER public API was found. "None exists" is an absence-of-evidence claim, not a confirmed one.
- Not confirmed whether AdER notifies cartelle via SEND in production as of Oct 2026. The June 2026 source implies the rollout was not complete.

## 5. PEC: legal value, duty to read, INI-PEC, reading by IMAP

### Takeaway
A PEC delivery to the domicilio digitale has the legal value of a raccomandata A/R. A notification is effective on delivery (or at SEND's 7-day mark), whether read or not, so a business owner bears the risk of not reading. Reading his own PEC through a standard IMAP client with his app password is technically supported and is how the vault's own `pec_*` tools already work: read-only, EXAMINE + BODY.PEEK, password from the Keychain. I found nothing forbidding a tool from doing this for the owner with his consent. Two constraints apply:
- GDPR, because the mailbox holds third parties' data;
- the provider's terms on credential custody.

### Cited Findings
- Not reading the PEC does not stop a PEC-notified act from taking effect. The delivery receipt certifies deposit in the mailbox, not reading. — [tributiconsulting (secondary)](https://tributiconsulting.it/cartelle-esattoriali/notifica/pec)
- SEND notifications reach recipients with an active domicilio digitale (PEC) and are perfected 7 days after delivery. A full, invalid or inactive PEC leads to perfection 15 days after the failed-delivery notice. — [notifichedigitali.it/perfezionamento](https://notifichedigitali.it/perfezionamento)
- PEC and domicilio digitale for professionals, and their use for notifications: [Mefop, Il professionista e il domicilio digitale](https://www.mefop.it/blog/blog-mefop/professionista-domicilio-digitale); [FiscoeTasse, atti tributari via PEC](https://www.fiscoetasse.com/approfondimenti/15726-le-nuove-regole-per-gli-atti-tributari-notificati-a-mezzo-pec.html)
- In this codebase, the vault's PEC tool is read-only IMAP: EXAMINE + BODY.PEEK, no SMTP code, app password fetched from the Keychain per request. — project CLAUDE.md (local, `mcp/src/pec.ts`)

### Inferences
- A full PEC mailbox is a real risk. Notifications then perfect at 15 days through the failed-delivery route, so the tool should watch mailbox quota.
- **IMAP access with an app password the owner creates is not credential sharing in the SPID sense.** It is ordinary mailbox access, and PEC providers (Aruba, Legalmail, Poste) document IMAP/POP setup for third-party clients. Reading-only, keeping the password in the owner's Keychain and running locally keeps the tool from becoming a processor of the PEC content.
- **A hosted (cloud) reader is a heavier commitment.** It would make the operator a responsabile del trattamento (art. 28 GDPR) for everything in the mailbox, including third parties' data. Using BODY.PEEK keeps the read state unchanged, so the owner still sees the messages as unread.

### Gaps
- Not re-verified this session: the statutory texts (art. 16 D.L. 185/2008 for companies; art. 5 D.L. 179/2012 for ditte individuali' PEC obligation; art. 37 D.L. 76/2020 sanctions; art. 6-bis CAD on INI-PEC; art. 48 CAD on PEC as raccomandata).
- Not checked: individual PEC providers' terms on automated or third-party IMAP access, or any provider that disables IMAP or requires OAuth.
- I found no Garante decision specifically about third-party tools reading a client's PEC.

## 6. PDND (Piattaforma Digitale Nazionale Dati): can a private company access e-services about a consenting user?

### Takeaway
Since 15 Jan 2026, PDND has been open to every company in the Registro Imprese, as fruitore and as erogatore, following the June 2025 AgID guidelines. Joining does not grant access to any PA e-service. Each erogatore must approve a fruizione request for a declared purpose with a legal basis. The fruitore is an autonomous data controller. I found no e-service that lets a private company pull a consenting citizen's debts or notifications. Even ANPR access opened on 1 Jul 2026 to PA and public-service operators, not to arbitrary private firms. PDND is not a practical route for a free tool today.

### Cited Findings
- "La PDND è aperta a tutte le imprese iscritte al Registro Imprese". Companies can act as erogatori and fruitori; news dated 15 Jan 2026, following the June 2025 AgID guidelines update. The page states no further conditions, costs or e-service list. — [interop.pagopa.it, Apertura della PDND ai privati](https://www.interop.pagopa.it/news/apertura-privati)
- New version of the PDND guidelines opening to private parties. — [Associazione Cittadinanza Digitale, 12 Jul 2025](https://www.associazionecittadinanzadigitale.org/2025/07/12/nuova-versione-linee-guida-pdnd/)
- Fruizione works as follows: browse the catalogue, submit a request stating the purposes (finalità), and get enabled only once the erogatore approves. Fruitori act as autonomous controllers, fully responsible for lawfulness, relevance, proportionality and security. — [PMI.it, PDND 2026](https://www.pmi.it/pubblica-amministrazione/riforma-pa/382391/piattaforma-digitale-nazionale-dati.html); [ANPR, Accesso ai dati](https://www.anagrafenazionale.interno.it/area-tecnica/accesso-ai-dati/)
- From 1 Jul 2026, ANPR data are available via PDND to PA and gestori di pubblici servizi, with access monitoring (anomaly indicators such as volumes 18:00–08:00 and holidays). — [ANPR news](https://www.anagrafenazionale.interno.it/anpr/notizie/accessi-ad-anpr-tramite-pdnd-da-parte-di-pa-e-gestori-di-pubblici-servizi/); [DSPT](https://www.dspt.it/anpr-pdnd-accesso-automatico-dati-anagrafici-vigilanza/)
- The Piano triennale targets 2,000 APIs in the PDND catalogue and 7,000 authorised fruizione requests for 2026. — [PMI.it](https://www.pmi.it/pubblica-amministrazione/riforma-pa/382391/piattaforma-digitale-nazionale-dati.html)
- SEND can take a PDND voucher as an extra auth layer for senders. — [search summary of the SEND FAQ](https://developer.pagopa.it/send/guides/knowledge-base/readme/inserimento-della-notifica/faq-inserimento-notifiche)

### Inferences
- **Consent alone does not get a private fruitore approved.** User consent is not by itself a legal basis an erogatore will accept: PA e-services are generally offered for institutional purposes defined by law. A private tool would need an erogatore (AdER, a Comune, SEND) to publish an e-service whose terms allow private fruitori for this purpose. I found no evidence of one.

### Gaps
- Not opened: the full June 2025 AgID guidelines text on private fruitori (whether the "consent of the data subject" route is contemplated, and the conditions).
- Not checked: the PDND catalogue for e-services with private-fruitore eligibility relevant to debts, notifications or payments.

## 7. Legal limits on a tool automating a SPID login for the owner

### Takeaway
SPID credentials are personal and must not be shared. AgID-linked material describes sharing as prohibited and says the holder answers for what a person given the credentials does. Holder duties of custody and exclusive use come from the SPID rules. A tool that stores Gennaro's SPID username, password or OTP and logs in for him (headless scraping of the SEND, AdER or IO web areas) breaches those rules. The legitimate alternatives are:
- the delegation mechanisms built into each service (SEND deleghe, AdER/AdE delega unica to the commercialista);
- the owner logging in himself, with the tool working only on what he exports or forwards.

### Cited Findings
- Sharing SPID credentials, for example at work, is described as prohibited though widespread. The holder can be held accountable for actions by the person given the credentials, who can also access the holder's personal data. — [Agenda Digitale, SPID per uso professionale](https://www.agendadigitale.eu/cittadinanza-digitale/identita-digitale/spid-per-uso-professionale-come-funziona-e-chi-puo-usarlo/) (via search summary)
- AgID's "obblighi dei gestori e dei titolari" document sets the identity providers' duties (secure delivery of credentials; prompt, free suspension or revocation and credential changes at the user's request; revocation after 24 months of inactivity or on death) and holder duties. I could not open the PDF (timeout) to quote the holder's exclusive-use clause. — [AgID, obblighi dei gestori e dei titolari (PDF)](https://www.agid.gov.it/sites/default/files/repository_files/regole_tecniche/obblighi_dei_gestori_e_dei_titolari.pdf)
- With professional SPID, identity providers must not allow authentication with the professional identity unless the service provider explicitly requested that type. — [Agenda Digitale](https://www.agendadigitale.eu/cittadinanza-digitale/identita-digitale/spid-per-uso-professionale-come-funziona-e-chi-puo-usarlo/)
- SEND deleghe require SPID for both delegante and delegato, so the delegate uses their own identity. — [Punto Informatico](https://www.punto-informatico.it/send-servizio-notifiche-digitali-pec-inad/)

### Inferences
- **A local tool where the owner logs in himself is defensible; holding his credentials is not.** The tool drives a browser on his machine, he types SPID and approves the OTP or push himself, and the tool then reads pages in his live session. That is close to "the owner using a browser extension". Storing his SPID secrets or replaying his OTP is "credential sharing", against the SPID rules and the IdP terms.
- **Delegation-first fits the vault.** This matches the vault's existing stance: credentials stored only as `personal` tier, never readable by service tokens. A free tool should design around delegation and the owner's own exports, not around holding SPID credentials.
- A data-scraping session from an automation could also trip IdP or portal anti-bot controls. Bypassing those would be a separate breach.

### Gaps
- Need the primary text: the holder obligations in the SPID regulation (DPCM 24 Oct 2014, art. on titolare duties; AgID "Regolamento recante le modalità attuative") with exact wording on exclusive custody and non-transfer. The PDF fetch failed.
- Not researched: each IdP's terms of service (Poste, Aruba, InfoCert, etc.) on automated or third-party use.
- Not researched: any Garante or AgID statement on "screen-scraping" PA portals on a user's behalf.
