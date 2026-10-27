# Existing tools and practices: how Italian micro-business owners know what they owe and when (status 2026-10-27)

Persona: Gennaro, pizzeria in Naples, ditta individuale, 3 employees, commercialista + consulente del lavoro, smartphone, WhatsApp, SPID, a PEC he rarely opens.

Method note: about 22 searches and fetches. Many hits were product marketing pages or 2021-2024 press. Every finding below has a date or a caution where it matters. Nothing comes from memory unless it is marked as an inference.

## 1. Consumer/SMB tools (Fiscozen, Qonto and neobanks, Fatture in Cloud, Aruba, TeamSystem/Danea, Agenzia delle Entrate scadenzario and app, impresa.italia.it, app IO, banks' F24 scheduling, ICS calendars)

### Takeaway
Every tool covers one silo. The tools that are personalized and know what has been paid either target forfettari (Fiscozen, Quickfisco) or hold only the payments made through them (Qonto, banks). The free public channels (app IO, Agenzia delle Entrate SMS/email, impresa italia, the INPS cassetto) are personal but each covers only its own ente, and mostly push notices rather than an "owed / done" ledger. Free calendars (ICS) are generic. Nothing found joins taxes, INPS, INAIL, TARI and Camera di Commercio for one business, with paid/unpaid status.

### Cited Findings

**Fiscozen (paid, aimed at forfettari)**
- The FAQ says users get advance notices well before deadlines. The dashboard section "Le tue tasse previste" updates in real time from the invoices issued — [Fiscozen FAQ](https://www.fiscozen.it/faq/)
- Fiscozen generates the F24 with codes and amounts but does not pay it. The user downloads it from the private area and pays via home banking or the Agenzia delle Entrate site — [Fiscozen FAQ](https://www.fiscozen.it/faq/)
- It calculates F24s for INPS gestione separata, artigiani/commercianti and the main casse professionali. A missed payment is handled by the dedicated consultant through ravvedimento operoso with a new F24 — [Fiscozen FAQ](https://www.fiscozen.it/faq/)
- Price: a third-party comparison lists the forfettario plan at €499/year or €49.90/month. That page is oddly dated (it refers to 2027), so the price is unconfirmed — [economia-italia.com](https://www.economia-italia.com/fiscozen-alternative)
- Competitor Quickfisco (forfettari) advertises reminders before each deadline with F24s already filled in — [Quickfisco strumenti](https://quickfisco.it/strumenti/)

**Qonto (paid business account)**
- F24 payment in the app: you fill the form and schedule it, Qonto debits the account on the chosen date and the receipt arrives a few days later. It supports F24 Ordinario, plus Semplificato/Predeterminato traced back to Ordinario (coverage dates from the 2021 launch) — [PagamentiDigitali 2021](https://www.pagamentidigitali.it/fintech/qonto-disponibile-online-il-pagamento-dei-tributi-f24/); [Qonto glossary F24](https://qonto.com/it/glossary/f24)
- "I24": the accountant can pay the client's F24 through Entratel/Fisconline, debiting the client's Qonto IBAN on the due date, using the client's codice fiscale/P.IVA and the F24 data. The account must hold enough funds — [Qonto support](https://support-it.qonto.com/hc/en-us/articles/23949934708753-Can-I-delegate-F24-payment-to-my-accountant); [AziendaBanca](https://www.aziendabanca.it/notizie/fintech-insurtech/qonto-f24)
- The receipt shows up in the cassetto fiscale (Agenzia delle Entrate) about 5 days after the payment date — [PagamentiDigitali, I24](https://www.pagamentidigitali.it/news/il-servizio-i24-di-qonto-delega-il-pagamento-di-f24-al-commercialista/)
- Qonto's "scadenzario fiscale" is a generic blog calendar with a downloadable .ics. The version found was for 2025 — [Qonto blog calendario fiscale](https://qonto.com/it/blog/finance/adempimenti-fiscali-e-pagamenti-il-calendario-fiscale)

**Fatture in Cloud (TeamSystem group)**
- The commercialista's account is "free forever". The accountant can upload F24s to the client's account, and the client gets a notification "per non scordarsi di pagarli". The page does not say whether the client sees paid/unpaid status — [Fatture in Cloud commercialisti](https://www.fattureincloud.it/commercialisti/)
- The client can invite the commercialista to manage invoices and F24s ("Commercialista connesso") — [Fatture in Cloud, commercialista connesso](https://www.fattureincloud.it/software-fatturazione/commercialista-connesso/)
- "Scadenzario" is listed as a general feature, with no detail for studio use — [Fatture in Cloud commercialisti](https://www.fattureincloud.it/commercialisti/)

**Danea Easyfatt / Aruba**
- Danea's blog has a post on collaborating with the commercialista. No F24 scadenzario or client F24 notification feature was found — [Danea blog](https://www.danea.it/blog/danea-news/easyfatt/page/4)
- No relevant results for Aruba Fatturazione Elettronica.

**Agenzia delle Entrate: app IO, SMS/email, "Le tue scadenze"**
- Agenzia delle Entrate notifications on app IO come in two kinds, "Comunicazioni per te" (e.g. a refund arriving) and "Le tue scadenze" (reminders for rate, contracts such as locazione, adempimenti). Launched 2024 — [Open, 2024-04-10](https://www.open.online/2024/04/10/app-io-notifiche-agenzia-entrate-scadenze-rimborsi); [InformazioneFiscale](https://www.informazionefiscale.it/rimborsi-scadenze-fiscali-avviso-agenzia-delle-entrate-notifica-app-io)
- SMS/email alerts: enter contacts in the reserved area under "Contatti" and choose which cases to be alerted about (deadlines, payments) — [InformazioneFiscale](https://www.informazionefiscale.it/rimborsi-scadenze-fiscali-avviso-agenzia-delle-entrate-notifica-app-io)
- In 2025 the Agenzia sent over 20 million IO notifications across more than 30 message types: delega to an intermediary (CAF/commercialista), IBAN for refunds, reminders to file the return, tassazione separata, and others. The 2026-2028 plan adds 2.4 million compliance letters in 2026. The IO message only says that a "comunicazione fiscale sensibile" is in the reserved area. IO had about 14 million active users in December 2025. Push requires the IO service to be enabled — [Sky TG24, 2026-02-17](https://tg24.sky.it/economia/2026/02/17/fisco-agenzia-entrate-app-io-notifiche-novita-2026)
- No source shows these notifications covering a business's periodic F24 (IVA, ritenute on employees' wages, INPS contributions). Examples are personal-taxpayer events — same source.

**app IO / pagoPA for TARI**
- The pagoPA service model for TARI on IO lets a comune send payment notices (amount, due date, "Vedi Avviso", pay in the app) and updates on extended due dates. Paper notices can be paid by scanning the QR/DataMatrix — [pagoPA service manual, TARI](https://docs.pagopa.it/manuale-servizi/v2.0-1/catalogo-dei-servizi-e-modelli/i-modelli-dei-servizi-piu-frequenti/tassa-sui-rifiuti-tari); [pagoPA KB pagamenti IO](https://docs.pagopa.it/kb-enti-pagamenti/domande-frequenti/domande-e-risposte-sui-pagamenti-in-io)
- Whether the Comune di Napoli sends TARI notices on IO is not confirmed (see Gaps).

**impresa italia (Camere di Commercio / InfoCamere "cassetto digitale dell'imprenditore")**
- Free, login with SPID/CIE/CNS. Official documents from the Registro Imprese (visura, atto costitutivo, bilancio), tracking of filings to the Registro Imprese and SUAP, and payment of the diritto annuale from the phone. Became an app in July 2024 — [AGI, 2024-07-24](https://www.agi.it/cronaca/news/2024-07-24/cassetto-digitale-app-imprenditori-impresa-italia-27221367/); [InfoCamere press release 2024](https://infocamere.it/dam/jcr:f9d78e8d-c166-40fe-9dcc-a0ef7243a4f1/2024_07_24_Comunicato%20stampa%20app%20impresa%20italia%20(1).pdf)
- The 2024 announcements promised personalized notifications on deadlines and a check of the company against the Chamber's obligations: PEC/domicilio digitale, bilancio filed, consistency of IVA status — [StartMag](https://www.startmag.it/innovazione/impresa-italia-il-cassetto-digitale-dellimprenditore-diventa-app); [CCIAA Riviere di Liguria](https://rivlig.camcom.gov.it/print/pdf/node/1433)
- The 2025 guide (Camera di commercio, July 2025 presentation) says: "Nuovi Servizi e Notifiche Push". The home page is a "Dashboard personalizzata" with a "vista sinottica su attività, strumenti, scadenze". The documents "Valigetta" gives notifications when documents are ready. It also says the app helps you monitor the company's "salute", manage the Diritto Annuale and receive personalized notifications — [impresa italia guida 2025 (PDF)](https://www.pnud.camcom.it/allegati/strum_impr/impresa_italia_guida_2025.pdf)
- From July 2025, every request for official documents triggers a push to the legal representative (a security feature) — [CCIAA PNUD guide / search summary](https://www.pnud.camcom.it/allegati/strum_impr/impresa_italia_guida_2025.pdf); [Unioncamere press release, Aug 2026](https://www.unioncamere.gov.it/sites/default/files/articoli/2026-08/17082026_com_impresaItalia%20%281%29.docx) (only a snippet was seen)
- Scope is Camera di Commercio only (diritto annuale, register obligations). Nothing found shows it covering taxes, INPS, INAIL or TARI.

**INPS (cassetto previdenziale artigiani e commercianti)**
- INPS message no. 798 of 5 March 2025 extends "Comunicazione bidirezionale" in the Cassetto Previdenziale to artigiani/commercianti. It lets users send queries with attachments and track their status, with email/SMS notifications on protocollazione and closing. Open to the holder and to intermediaries with an active delega — [MySolution, 2025-03-06](https://www.mysolution.it/lavoro/informazioni/news/2025/03/06/inps-novita-sul-cassetto-previdenziale-del-contribuente-per-i-lavoratori-autonomi-artigiani-e-commercianti); [Edotto](https://edotto.com/articolo/artigiani-e-commercianti-nuova-comunicazione-bidirezionale)
- The cassetto shows the payments made on the position and the debit/credit situation (2012 source) — [DottrinaLavoro](https://www.dottrinalavoro.it/notizie-c/inps-cassetto-previdenziale-per-artigiani-e-commercianti-chiarimenti-tecnico-applicativi)
- 2018: INPS emailed alerts (avvisi bonari) for unpaid rate to holders and intermediaries who registered an email in the cassetto. If still unpaid, an avviso di addebito with titolo esecutivo follows. Dated; current practice unverified — [DottrinaLavoro 2018](https://www.dottrinalavoro.it/notizie-c/inps-gestione-artigiani-e-commercianti-avvisi-bonari-rata-con-scadenza-febbraio-2018)

**Generic calendars and ICS**
- Tot gives a 2026 partita IVA calendar as an .ics sent by email after a form (import, not a live subscription) — [Tot blog, scadenze fiscali 2026](https://tot.money/blog/scadenze-fiscali-2026)
- The community Python package "scadenze" exports 2026 Italian deadlines (IVA, ritenute, INPS, IMU, dichiarazioni, acconti) to .ics, with weekend/holiday shifts applied. Unofficial — [PyPI scadenze](https://pypi.org/project/scadenze/)
- Fiscal Focus publishes monthly scadenzari for professionals, with no importable file seen — [Fiscal Focus](https://www.fiscal-focus.it/fisco/info-fisco/scadenzario/calendario-fiscale-modello-redditi-persone-fisiche,3,87038)

**Banks**
- Home banking (CBI) is one of the three legal channels for partite IVA, with F24 online direct and intermediari via Entratel (old 2006 framing) — [FiscoeTasse](https://www.fiscoetasse.com/approfondimenti/387-l-agenzia-precisa-pagamenti-f24-anche-mediante-l-home-banking.html)
- Since 1 July 2024 (Legge di Bilancio 2024), F24s that offset INPS/INAIL credits can no longer go through bank services (per an Edupass software guide) — [Edupass corso F24](https://www.edupass.it/StreamFile/Resources/materialevideocorsiriservati/corsof24.pdf)

### Inferences
- Gennaro's case: a pizzeria with 3 employees almost certainly exceeds the forfettario cap on staff costs (an inference: the €20k limit for lavoro dipendente is not sourced here). So Fiscozen/Quickfisco-style "personal tax feed" products probably don't serve him.
- The data that would make a feed personal and "done-aware" already exists in public silos, each behind SPID: the cassetto fiscale (F24 receipts, including those paid by the commercialista via Entratel — sourced via the Qonto I24 receipt path), the INPS cassetto (debit/credit position), pagoPA/IO (TARI and other avvisi), impresa italia (diritto annuale, register compliance). None joins them.
- Bank and neobank calendars know "paid" only for F24s routed through that bank, and know nothing of what the commercialista will file next.
- Public notifications are pushed, not a ledger. IO messages often only point to the reserved area. A busy owner gets "something is in your cassetto", not "you owe €X on the 16th and it's done".

### Gaps
- No primary page confirmed that Fiscozen supports regime semplificato / ditte with employees (2026).
- No source showed Qonto's or other neobanks' (Revolut Business, Finom, Satispay Business) built-in tax calendars beyond static blog calendars. Bank-side F24 scheduling at Intesa, UniCredit, etc. was not checked.
- Whether the Comune di Napoli sends TARI avvisi via IO is not found.
- No 2026 confirmation of which impresa italia notifications actually fire (the guide says "scadenze" on the dashboard and push notifications, without listing which deadlines).
- INAIL (autoliquidazione 16 February, alerts) was not researched. No source found on INAIL client notifications.
- No source on the INPS app (mobile) giving business payment alerts.

## 2. Professional side: studio software, client portals, how F24s reach clients, share paid via the commercialista's addebito

### Takeaway
The big studio vendors sell a client app or portal as an add-on (TeamSystem "Digital Box" and a TeamSystem portal app for studios and clients; Fatture in Cloud F24 upload with notification). Small studios also ship white-label apps that show each F24 and let the client approve or refuse the debit. Adoption is unknown. Delivery of F24s to clients by email or WhatsApp, and the share paid by the intermediary's addebito versus the client, could not be quantified from public sources.

### Cited Findings
- TeamSystem Studio: "Digital Box, estensione naturale del gestionale di Studio", to exchange information, documents, data, scadenze and KPIs with clients in real time. The studio suite lists "App mobile per lo Studio e per i clienti" and a client portal for payments — [TeamSystem Studio, collaborazione con i clienti](https://www.teamsystem.com/commercialisti/teamsystem-studio/funzionalita/collaborazione-con-i-clienti/); [TeamSystem studio digitale](https://www.teamsystem.com/commercialisti/teamsystem-studio/funzionalita/studio-digitale/)
- Studio white-label apps: "Telematica E Servizi" (iOS, developer listed as an individual): "Ricevi una notifica quando è pronto un nuovo modello F24 per una delle tue aziende" — [App Store](https://apps.apple.com/app/id6754166914)
- Another studio app ("Studio De Faveri", iOS): notifications on the client's payment deadlines with the detail of each tax, the option to approve or refuse the debit, plus a deadline agenda and documents — [App Store](https://apps.apple.com/it/app/id1441319969)
- Industry guidance on client portals: each obligation should have a status (richiesto, in attesa, ricevuto, firmato, pagato). The post frames the problem as "lo studio rincorre i clienti" — [Optlyx guide](https://optlyx.com/guida-portale-clienti-studio-commercialista) (vendor content)
- International players are marketing into Italy: TaxDome promotes its client mobile app (Italian-language blog) — [TaxDome IT](https://taxdome.com/it-it/blog/rivoluzionare-lesperienza-cliente-perche-lapp-mobile-per-clienti-di-taxdome-e-nella-top-100-degli-app-store-con-un-punteggio-di-49-5-in-oltre-4000-recensioni)
- Delega/addebito mechanics: since a 2007 provvedimento, Entratel intermediaries can send clients' F24s with a single debit ("addebito unico") on the intermediary's own account. The standard path debits the client's account with the client's IBAN. A cumulative file holds up to 999 F24s — [FiscoeTasse, addebito unico](https://www.fiscoetasse.com/normativa-prassi/10020-pagamenti-f24-da-parte-degli-intermediari-con-addebito-unico.html); [AdE convenzione 2013, allegato 2](https://www.agenziaentrate.gov.it/portale/documents/20143/252483/Nuova+convenzione+2013+allegato+2_Allegato+2+-+F24+cumulativo+online.pdf/d8af2448-318d-1339-435f-549a7d6d81d7)
- Authorization form: the client signs a delega authorizing the intermediary to transmit F24s by telematic means — [MisterFisco facsimile](https://www.misterfisco.it/wp-content/uploads/2014/02/Autorizzazione-alla-trasmissione-telematica-dei-modelli-di-versamento-unificato-F24-fac-–-simile.pdf)
- Liability when a delegated F24 bounces: Fiscozen reports a Cassazione ruling (2026) that the bank was not liable for a rejected F24 when it promptly told the intermediary; the commercialista's failure to warn the client broke the causal link — [Fiscozen news](https://www.fiscozen.it/news/f24-inviati-dagli-intermediari-quando-la-banca-non-e-responsabile-per-lo-scarto/) (ruling number not checked)
- Consulente del lavoro: employees' contributions are due by the 16th of the following month, even if wages were not paid. The employer alone is liable for paying them, the employee's share included — [INPS, pagamento contributi](https://inps.it/it/it/inps-comunica/diritti-e-obblighi-in-materia-di-sicurezza-sociale-nell-unione-e/per-le-imprese/pagamento-dei-contributi-previdenziali.html)
- A typical payroll service bundle: monthly cedolini, LUL and F24 — [Fondazione Alma Ricerca, servizi richiesti](https://fondazionealmaricerca.unibo.it/it/allegati/files/All2_elencodeiservizirichiesti_Lavoro.pdf/@@download/file/All2_elencodeiservizirichiesti_Lavoro.pdf) (a tender document, not market data)

### Inferences
- For Gennaro there are two streams of F24 a month: the consulente del lavoro's (ritenute and INPS for employees, by the 16th) and the commercialista's (IVA, acconti, INPS artigiani/commercianti fixed rate). They usually come from two separate studios and two separate pieces of software. Neither studio's app, if one exists, shows the other's.
- When the studio pays by Entratel addebito, the owner may never see the F24 before the debit. The only independent proof is the receipt in the cassetto fiscale ~5 days later. A feed that reads the cassetto fiscale would know "done" whoever paid.
- The studio apps found (approve/refuse debit, F24 detail) are the closest thing to a personal "owed and done" feed. But they are per studio, cover only what that studio prepares, and are not free to the studio.

### Gaps
- No vendor documentation found for Zucchetti (e.g. client portals), Wolters Kluwer (Genya/ArcaEvolution client tools), Profis/Sistemi or GB showing a client F24 feed. Agyo / TS Digital names were not confirmed in current TeamSystem pages.
- No data found on the share of micro-businesses whose F24s the intermediary pays by addebito (delega Entratel) versus the owner paying (home banking, F24 web, bank counter). The Agenzia delle Entrate's annual reports might have F24 channel breakdowns; not found.
- No source on how often studios send F24s by email, by WhatsApp or by PEC. Anecdotally this is common, but unsourced here.

## 3. What owners complain about (surprises, missed deadlines, fines, not knowing what the commercialista did)

### Takeaway
Searches did not surface the specific Reddit or Facebook threads, so owner-voice evidence is thin. Trade and association sources confirm the scale (dozens of fiscal deadlines a year, hundreds of hours of red tape), the penalty path when a payment is missed, and the classic "acconti surprise".

### Cited Findings
- Associations' figures: about 70 fiscal deadlines a year and 269 hours a year on bureaucracy (Rete Imprese Italia/CER study, cited by Confcommercio Potenza; study year not given) — [LavoriPubblici](https://www.lavoripubblici.it/news/stampa/16016)
- CGIA: fiscal red tape costs entrepreneurs about €3 billion a year. It cites 270 deadlines in 15 days in one month (date unclear) — [01net](https://www.01net.it/?p=159461)
- Astrid, Osservatorio Burocrazia 2025: about 313 hours of work a year on adempimenti, on a sample of 1,000 firms — search summary of [Wall Street Italia](https://www.wallstreetitalia.com/?p=619330) / [La Notizia](https://www.lanotiziagiornale.it/?p=138592) (not opened)
- Format Research hearing on bureaucracy at AGCOM, 6 June 2026 (not opened) — [AGCOM PDF](https://www.agcom.it/sites/default/files/documenti/audizioni_parlamentari/06.06.2026%20-%20Burocrazia%20Leva%20per%20la%20crescita%20delle%20imprese%20o%20ostacolo%20allo%20sviluppo%20-%20Format%20Research.pdf)
- 2026: the confederations (incl. CNA Emilia-Romagna) asked to move the ISA taxpayers' payments to 20 July 2026, or 19 August with a 0.4% surcharge, citing operational difficulties. So the dates move yearly — [CNA Emilia-Romagna](https://emiliaromagna.cna.it/?p=55877)
- Penalty path: unpaid, then ravvedimento operoso (reduced sanction, "Ravv." box on the F24); otherwise an avviso bonario; if ignored, a cartella with higher sanctions — [IlCommercialistaOnline](https://www.ilcommercialistaonline.it/pagamento-f24-ritardo/)
- From 1 September 2024 the base sanction for omesso versamento is 25%, reduced for payment within 14 days — [Quickfisco blog](https://quickfisco.it/blog/regime-forfettario/ravvedimento-operoso-cose-come-funziona-e-novita-dal-2024) (blog; verify on AdE)
- INPS: the forum answer says sprint ravvedimento doesn't apply to INPS; interest for late payment is due (unverified) — [FiscoeTasse forum](https://www.fiscoetasse.com/forum/goto/post?id=414499)
- Surprise payments: partita IVA taxes are paid on 30 June (saldo + first acconto) and 30 November (second acconto, not instalable). The IVA acconto is due 27 December — [Fiscozen guide (updated June 2026)](https://www.fiscozen.it/guide/quando-si-pagano-le-tasse-con-la-partita-iva/); [Money.it (2022)](https://www.money.it/acconti-partita-iva-come-funzionano)
- System failures also shift dates: Fiscozen reported an F24 system malfunction (2024) with forms rejected for "squadratura contabile sezione erario" and deadlines moved — [Fiscozen news](https://www.fiscozen.it/news/problemi-al-sistema-f24-rimandate-le-scadenze-per-le-partite-iva/)

### Inferences
- The "not knowing what the commercialista did" complaint is structurally plausible: with Entratel addebito, the client is told after the fact, if at all. The Cassazione case above shows that when an F24 is rejected, the warning goes to the intermediary, not the owner.
- Moving dates (proroghe, system failures) are a reason a static ICS calendar fails.

### Gaps
- No Reddit (r/italy, r/ItaliaPersonalFinance), Facebook group or app-store review quotes were retrieved. Search for them directly if owner voice is needed.
- No survey found that measures how many micro-businesses miss deadlines or pay ravvedimento each year.

## 4. Italian startups and public projects that tried a personalized obligations feed (once-only, fascicolo d'impresa, cassetto digitale)

### Takeaway
The public sector is the most active builder: the Camere di Commercio's impresa italia (Chamber obligations, with a dashboard of scadenze and push notifications since the 2025 redesign) and the Agenzia delle Entrate's IO notifications ("Le tue scadenze", compliance letters from 2026), each limited to its own ente. No Italian startup with a cross-ente personal obligations feed for micro-businesses turned up, and none that failed either.

### Cited Findings
- impresa italia: the 2024 promise of personalized deadline notifications plus a "stato di salute" check against Chamber obligations — [StartMag](https://www.startmag.it/innovazione/impresa-italia-il-cassetto-digitale-dellimprenditore-diventa-app); the 2025 redesign with a dashboard of scadenze and push notifications — [guida 2025](https://www.pnud.camcom.it/allegati/strum_impr/impresa_italia_guida_2025.pdf)
- impresa italia's 2025 presentation links it to PDND Interoperabilità (the national data-exchange platform) — [guida 2025](https://www.pnud.camcom.it/allegati/strum_impr/impresa_italia_guida_2025.pdf)
- Agenzia delle Entrate: the 2026-2028 plan aims to consolidate most tax communications into IO — [Sky TG24](https://tg24.sky.it/economia/2026/02/17/fisco-agenzia-entrate-app-io-notifiche-novita-2026)
- Private "deadline reminder" offers exist only inside paid products (Fiscozen, Quickfisco) or studio apps — sources above.

### Inferences
- PDND is the plausible plumbing for a once-only, cross-ente view, but no product found exposes a combined business ledger to the owner.
- A free tool would compete mainly with inertia ("the commercialista handles it") and with per-ente public apps that are free but fragmented.

### Gaps
- No source found on the EU Single Digital Gateway / once-only technical system as applied to Italian micro-business obligations.
- No defunct Italian startup in this space was identified. Absence of evidence, not evidence of absence.
- The SUAP/fascicolo d'impresa (DPR 160/2010) as a source of a business's permit obligations (e.g. a pizzeria's SCIA, HACCP) was not researched here.
