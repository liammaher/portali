import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import {
  decodeText, envelopeSender, isCalendarDate, listFolders, listMessages, parseDaticert, parsePec, pecAttachment,
  pecConfig, pecKind, transportHeaders, MAX_TEXT_CHARS,
} from "./pec.js";
import { safeFilename, writeNewFile } from "./files.js";
import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const crlf = (s: string) => s.replace(/\r?\n/g, "\r\n");

const DATICERT_CONSEGNA = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE postacert SYSTEM "Postacert.dtd">
<postacert tipo="posta-certificata" errore="nessuno">
  <intestazione>
    <mittente>protocollo@pec.comune.napoli.it</mittente>
    <destinatari tipo="certificato">mario.rossi@pec.it</destinatari>
    <destinatari tipo="esterno">altro@example.com</destinatari>
    <risposte>protocollo@pec.comune.napoli.it</risposte>
    <oggetto>Avviso TARI 2026 &amp; rate</oggetto>
  </intestazione>
  <dati>
    <gestore-emittente>ARUBA PEC S.p.A.</gestore-emittente>
    <data zona="+0100">
      <giorno>25/10/2026</giorno>
      <ora>09:05:07</ora>
    </data>
    <identificativo>opec21011.20261025090507.12345.67.1.89@pec.aruba.it</identificativo>
    <msgid>&lt;abc123@comune.napoli.it&gt;</msgid>
  </dati>
</postacert>
`;

// A certified message as Aruba delivers it: an S/MIME-signed envelope holding
// a short notice, daticert.xml and the original message as postacert.eml.
const CERTIFIED = crlf(`Return-Path: <posta-certificata@pec.aruba.it>
From: "Per conto di: protocollo@pec.comune.napoli.it" <posta-certificata@pec.aruba.it>
Reply-To: protocollo@pec.comune.napoli.it
To: mario.rossi@pec.it
Subject: POSTA CERTIFICATA: Avviso TARI 2026 & rate
Date: Sun, 25 Oct 2026 09:05:07 +0100
X-Trasporto: posta-certificata
X-Riferimento-Message-ID: <abc123@comune.napoli.it>
MIME-Version: 1.0
Content-Type: multipart/signed; protocol="application/pkcs7-signature"; micalg=sha-256; boundary="SIGNED"

--SIGNED
Content-Type: multipart/mixed; boundary="MIXED"

--MIXED
Content-Type: text/plain; charset=UTF-8

Messaggio di posta certificata
Il giorno 25/10/2026 alle ore 09:05:07 (+0100) il messaggio "Avviso TARI 2026 & rate" e' stato inviato da "protocollo@pec.comune.napoli.it"
--MIXED
Content-Type: application/xml; name="daticert.xml"
Content-Disposition: inline; filename="daticert.xml"

${DATICERT_CONSEGNA}
--MIXED
Content-Type: message/rfc822; name="postacert.eml"
Content-Disposition: inline; filename="postacert.eml"

From: Comune di Napoli <protocollo@pec.comune.napoli.it>
To: mario.rossi@pec.it
Subject: Avviso TARI 2026 & rate
Date: Sun, 25 Oct 2026 09:05:00 +0100
Message-ID: <abc123@comune.napoli.it>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="INNER"

--INNER
Content-Type: text/plain; charset=UTF-8

Gentile contribuente, in allegato l'avviso di pagamento.
--INNER
Content-Type: application/pdf; name="avviso.pdf"
Content-Disposition: attachment; filename="avviso.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQKJcOkw7zDtsOfCg==
--INNER--
--MIXED--
--SIGNED
Content-Type: application/pkcs7-signature; name="smime.p7s"
Content-Disposition: attachment; filename="smime.p7s"
Content-Transfer-Encoding: base64

MIAGCSqGSIb3DQEHAqCAMIACAQEx
--SIGNED--
`);

// A short delivery receipt for a message the user sent: no postacert.eml.
const RECEIPT = crlf(`From: posta-certificata@pec.aruba.it
To: mario.rossi@pec.it
Subject: CONSEGNA: Dichiarazione TARI
Date: Sun, 25 Oct 2026 11:00:02 +0100
X-Ricevuta: avvenuta-consegna
X-Riferimento-Message-ID: <sent1@pec.it>
MIME-Version: 1.0
Content-Type: multipart/signed; protocol="application/pkcs7-signature"; micalg=sha-256; boundary="SIGNED"

--SIGNED
Content-Type: multipart/mixed; boundary="MIXED"

--MIXED
Content-Type: text/plain; charset=UTF-8

Ricevuta di avvenuta consegna
--MIXED
Content-Type: application/xml; name="daticert.xml"
Content-Disposition: inline; filename="daticert.xml"

<postacert tipo="avvenuta-consegna" errore="nessuno"><intestazione><mittente>mario.rossi@pec.it</mittente><destinatari tipo="certificato">entrate.tari-tares-tarsu@pec.comune.napoli.it</destinatari><oggetto>Dichiarazione TARI</oggetto></intestazione><dati><gestore-emittente>ARUBA PEC S.p.A.</gestore-emittente><data zona="+0100"><giorno>25/10/2026</giorno><ora>11:00:01</ora></data><identificativo>opec1@pec.aruba.it</identificativo><msgid>&lt;sent1@pec.it&gt;</msgid><ricevuta tipo="breve"/><consegna>entrate.tari-tares-tarsu@pec.comune.napoli.it</consegna></dati></postacert>
--MIXED--
--SIGNED
Content-Type: application/pkcs7-signature; name="smime.p7s"
Content-Disposition: attachment; filename="smime.p7s"
Content-Transfer-Encoding: base64

MIAGCSqGSIb3DQEHAqCAMIACAQEx
--SIGNED--
`);

beforeEach(() => {
  for (const k of ["PEC_ADDRESS", "PEC_KEYCHAIN_SERVICE", "PEC_IMAP_HOST", "PEC_IMAP_PORT"]) delete process.env[k];
});

test("pecConfig is off without PEC_ADDRESS and defaults to Aruba", () => {
  assert.equal(pecConfig(), undefined);
  process.env.PEC_ADDRESS = "mario.rossi@pec.it";
  process.env.PEC_KEYCHAIN_SERVICE = "pvault-pec";
  assert.deepEqual(pecConfig(), {
    address: "mario.rossi@pec.it", service: "pvault-pec", host: "imaps.pec.aruba.it", port: 993,
  });
  process.env.PEC_IMAP_HOST = "imap.example.it";
  process.env.PEC_IMAP_PORT = "nope";
  assert.equal(pecConfig()?.host, "imap.example.it");
  assert.equal(pecConfig()?.port, 993);
});

test("without a Keychain service, tools explain how to store the app password", async () => {
  process.env.PEC_ADDRESS = "mario.rossi@pec.it";
  await assert.rejects(listFolders(pecConfig()!), (e: Error) => {
    assert.match(e.message, /PEC_KEYCHAIN_SERVICE is not set/);
    assert.match(e.message, /security add-generic-password -U -a mario\.rossi@pec\.it -s pvault-pec -w/);
    assert.match(e.message, /never ask for it in chat/);
    return true;
  });
  process.env.PEC_KEYCHAIN_SERVICE = "bad name; rm -rf";
  await assert.rejects(listFolders(pecConfig()!), /not a valid Keychain service name/);
});

test("pecKind tells certified mail, receipts and anomalies apart", () => {
  assert.equal(pecKind({ trasporto: "posta-certificata" }), "posta-certificata");
  assert.equal(pecKind({ trasporto: "posta-certificata", ricevuta: "Avvenuta-Consegna" }), "ricevuta-avvenuta-consegna");
  assert.equal(pecKind({ ricevuta: "accettazione" }), "ricevuta-accettazione");
  assert.equal(pecKind({ trasporto: "errore" }), "anomalia");
  assert.equal(pecKind({}), "email");
});

test("transportHeaders reads folded header lines", () => {
  const raw = "X-Ricevuta:\r\n avvenuta-consegna\r\nX-Trasporto: posta-certificata\r\n\r\n";
  assert.deepEqual(transportHeaders(raw), { trasporto: "posta-certificata", ricevuta: "avvenuta-consegna" });
  assert.deepEqual(transportHeaders(""), { trasporto: undefined, ricevuta: undefined });
});

test("envelopeSender unwraps 'Per conto di:'", () => {
  const pec = "posta-certificata";
  assert.equal(
    envelopeSender([{ name: "Per conto di: protocollo@pec.comune.napoli.it", address: "posta-certificata@pec.aruba.it" }], undefined, pec),
    "protocollo@pec.comune.napoli.it",
  );
  assert.equal(envelopeSender([{ address: "posta-certificata@pec.aruba.it" }], [{ address: "x@pec.it" }], pec), "x@pec.it");
  assert.equal(envelopeSender([{ address: "plain@example.com" }], undefined, "email"), "plain@example.com");
  // Outside the provider's envelope, the display name and Reply-To are the sender's choice.
  assert.equal(
    envelopeSender([{ address: "scam@evil.example" }], [{ address: "protocollo@pec.comune.napoli.it" }], pec),
    "scam@evil.example",
  );
  assert.equal(
    envelopeSender([{ name: "Per conto di: protocollo@pec.comune.napoli.it", address: "scam@evil.example" }], undefined, pec),
    "scam@evil.example",
  );
  // So is a posta-certificata@ address on a message without PEC transport headers.
  assert.equal(
    envelopeSender([{ name: "Per conto di: protocollo@pec.comune.napoli.it", address: "posta-certificata@evil.example" }],
      [{ address: "protocollo@pec.comune.napoli.it" }], "email"),
    "posta-certificata@evil.example",
  );
});

test("parseDaticert reads the certification record", () => {
  const c = parseDaticert(DATICERT_CONSEGNA);
  assert.equal(c.tipo, "posta-certificata");
  assert.equal(c.errore, "nessuno");
  assert.equal(c.mittente, "protocollo@pec.comune.napoli.it");
  assert.deepEqual(c.destinatari, [
    { indirizzo: "mario.rossi@pec.it", tipo: "certificato" },
    { indirizzo: "altro@example.com", tipo: "esterno" },
  ]);
  assert.equal(c.oggetto, "Avviso TARI 2026 & rate");
  assert.equal(c.gestore, "ARUBA PEC S.p.A.");
  assert.equal(c.data, "2026-10-25T09:05:07+01:00");
  assert.equal(c.msgid, "<abc123@comune.napoli.it>");
  assert.equal(c.consegna, undefined);
});

test("parsePec unwraps a certified message from its envelope", async () => {
  const p = await parsePec(Buffer.from(CERTIFIED));
  assert.equal(p.kind, "posta-certificata");
  assert.equal(p.certificazione?.mittente, "protocollo@pec.comune.napoli.it");
  assert.equal(p.message.subject, "Avviso TARI 2026 & rate");
  assert.deepEqual(p.message.from, ["Comune di Napoli <protocollo@pec.comune.napoli.it>"]);
  assert.match(p.message.text, /in allegato l'avviso di pagamento/);
  assert.deepEqual(p.message.attachments.map((a) => [a.index, a.filename, a.content_type]), [
    [0, "avviso.pdf", "application/pdf"],
  ]);
  assert.equal(p.envelope?.subject, "POSTA CERTIFICATA: Avviso TARI 2026 & rate");

  const pdf = await pecAttachment(Buffer.from(CERTIFIED), 0);
  assert.equal(pdf.filename, "avviso.pdf");
  assert.ok(pdf.content.subarray(0, 5).equals(Buffer.from("%PDF-")));
  await assert.rejects(pecAttachment(Buffer.from(CERTIFIED), 1), /no attachment 1; this message has 1/);
});

test("parsePec reads a receipt and hides the PEC wrapping files", async () => {
  const p = await parsePec(Buffer.from(RECEIPT));
  assert.equal(p.kind, "ricevuta-avvenuta-consegna");
  assert.equal(p.certificazione?.tipo, "avvenuta-consegna");
  assert.equal(p.certificazione?.ricevuta, "breve");
  assert.equal(p.certificazione?.consegna, "entrate.tari-tares-tarsu@pec.comune.napoli.it");
  assert.equal(p.certificazione?.data, "2026-10-25T11:00:01+01:00");
  assert.match(p.message.text, /avvenuta consegna/);
  assert.deepEqual(p.message.attachments, []);
});

test("parsePec handles plain mail and cuts long bodies", async () => {
  const body = "a".repeat(MAX_TEXT_CHARS + 10);
  const raw = crlf(`From: someone@example.com\nTo: mario.rossi@pec.it\nSubject: hi\n\n${body}\n`);
  const p = await parsePec(Buffer.from(raw));
  assert.equal(p.kind, "email");
  assert.equal(p.envelope, undefined);
  assert.equal(p.certificazione, undefined);
  assert.match(p.message.text, /\[… cut: 10 more characters\]$/);
});

test("safeFilename keeps a bare, visible file name", () => {
  assert.equal(safeFilename("avviso.pdf", "x"), "avviso.pdf");
  assert.equal(safeFilename("../../.ssh/id_rsa", "x"), "_.._.ssh_id_rsa");
  assert.equal(safeFilename(".hidden", "x"), "hidden");
  assert.equal(safeFilename("a\u0000b\nc.pdf", "x"), "abc.pdf");
  assert.equal(safeFilename(undefined, "pec-1-0"), "pec-1-0");
  assert.equal(safeFilename("...", "pec-1-0"), "pec-1-0");
});

test("writeNewFile never replaces a file and is owner-only", async () => {
  const dir = await mkdtemp(join(tmpdir(), "pec-save-"));
  try {
    const first = await writeNewFile(dir, "avviso.pdf", Buffer.from("one"));
    const second = await writeNewFile(dir, "avviso.pdf", Buffer.from("two"));
    assert.equal(first, join(dir, "avviso.pdf"));
    assert.equal(second, join(dir, "avviso (2).pdf"));
    assert.equal(await readFile(first, "utf8"), "one");
    assert.equal((await stat(second)).mode & 0o777, 0o600);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

// --- Regressions from review ---

test("an .eml attached to ordinary mail is not unwrapped", async () => {
  const sentCopy = crlf(`From: mario.rossi@pec.it
To: entrate.tari-tares-tarsu@pec.comune.napoli.it
Subject: Dichiarazione TARI
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="B"

--B
Content-Type: text/plain; charset=UTF-8

In allegato la dichiarazione.
--B
Content-Type: application/pdf; name="modulo.pdf"
Content-Disposition: attachment; filename="modulo.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQK
--B
Content-Type: message/rfc822; name="avviso.eml"
Content-Disposition: attachment; filename="avviso.eml"

From: x@example.com
Subject: Avviso originale

ciao
--B--
`);
  const p = await parsePec(Buffer.from(sentCopy));
  assert.equal(p.kind, "email");
  assert.equal(p.message.subject, "Dichiarazione TARI");
  assert.match(p.message.text, /In allegato la dichiarazione/);
  assert.deepEqual(p.message.attachments.map((a) => a.filename), ["modulo.pdf", "avviso.eml"]);
  assert.equal((await pecAttachment(Buffer.from(sentCopy), 0)).filename, "modulo.pdf");
});

test("a failed delivery keeps the provider's notice in envelope.text", async () => {
  const raw = RECEIPT.replace("X-Ricevuta: avvenuta-consegna", "X-Ricevuta: errore-consegna")
    .replace("Ricevuta di avvenuta consegna", "Il messaggio non e' stato consegnato: casella piena");
  const p = await parsePec(Buffer.from(raw));
  assert.equal(p.kind, "ricevuta-errore-consegna");
  assert.match(p.envelope?.text ?? "", /casella piena/);
});

test("daticert.xml is decoded in its declared encoding", async () => {
  const xml = Buffer.concat([
    Buffer.from('<?xml version="1.0" encoding="ISO-8859-1"?><postacert tipo="posta-certificata"><intestazione><oggetto>Citt'),
    Buffer.from([0xe0]),
    Buffer.from("</oggetto></intestazione></postacert>"),
  ]);
  const raw = Buffer.concat([
    Buffer.from(crlf(`From: posta-certificata@pec.aruba.it
X-Trasporto: posta-certificata
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="B"

--B
Content-Type: application/xml; name="daticert.xml"
Content-Disposition: inline; filename="daticert.xml"
Content-Transfer-Encoding: base64

${xml.toString("base64")}
--B--
`)),
  ]);
  assert.equal((await parsePec(raw)).certificazione?.oggetto, "Città");
  assert.equal(decodeText(Buffer.from([0x43, 0xe0]), "iso-8859-1"), "Cà");
  assert.equal(decodeText(Buffer.from("ok"), "no-such-charset"), "ok");
});

test("a bad character reference doesn't make daticert unreadable", () => {
  const c = parseDaticert("<postacert><intestazione><oggetto>a &#x110000; b &#65;</oggetto></intestazione></postacert>");
  assert.equal(c.oggetto, "a &#x110000; b A");
});

test("an empty transport header doesn't swallow the next line", () => {
  assert.deepEqual(transportHeaders("X-Ricevuta:\r\nX-Trasporto: posta-certificata\r\n"), {
    trasporto: "posta-certificata",
    ricevuta: "",
  });
  assert.equal(pecKind(transportHeaders("X-Ricevuta:\r\nX-Trasporto: posta-certificata\r\n")), "posta-certificata");
});

test("dates must be real days, checked before connecting", async () => {
  assert.ok(isCalendarDate("2026-02-28"));
  assert.ok(isCalendarDate("2028-02-29"));
  for (const bad of ["2026-02-30", "2026-13-01", "2026-1-01", "26-01-01", ""]) assert.equal(isCalendarDate(bad), false, bad);
  process.env.PEC_ADDRESS = "mario.rossi@pec.it";
  await assert.rejects(
    listMessages(pecConfig()!, { folder: "INBOX", limit: 5, since: "2026-13-01" }),
    /"2026-13-01" is not a real date/,
  );
});

test("safeFilename never yields a hidden name and keeps the extension", () => {
  assert.equal(safeFilename(" .bashrc", "x"), "bashrc");
  assert.equal(safeFilename(". .zshrc", "x"), "zshrc");
  assert.equal(safeFilename("fdp‮exe.pdf", "x"), "fdpexe.pdf");
  const long = safeFilename(`${"a".repeat(300)}.pdf`, "x");
  assert.equal(long.length, 200);
  assert.ok(long.endsWith(".pdf"));
});
