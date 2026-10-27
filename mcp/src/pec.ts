/**
 * Read-only access to the user's PEC (posta elettronica certificata) mailbox
 * over IMAP. There is deliberately no SMTP code: a PEC message has the legal
 * weight of a registered letter, so sending stays with the user.
 *
 * Mailboxes are opened with EXAMINE (read-only) and bodies fetched with
 * BODY.PEEK, so nothing is marked as read, moved or deleted.
 *
 * The password is the "password per programmi di posta" generated in the
 * provider's account panel, read from the macOS Keychain on every request
 * (never cached), like the vault token.
 */
import { ImapFlow, type FetchMessageObject, type SearchObject } from "imapflow";
import { simpleParser, type AddressObject, type Attachment, type ParsedMail } from "mailparser";
import {
  findGenericPassword, KEYCHAIN_SERVICE_PATTERN, SECRET_KIND, SECRET_STORE, SECURITY_ITEM_NOT_FOUND, storeToolMissing,
} from "./keychain.js";
import { lookupFailure } from "./client.js";

export interface PecConfig {
  address: string;
  service: string | undefined;
  host: string;
  port: number;
}

/** Largest message pec_read will download and parse. */
export const MAX_MESSAGE_BYTES = 30 * 1024 * 1024;
/** Longest body text returned; the rest is cut with a note. */
export const MAX_TEXT_CHARS = 20_000;

const CONNECT_TIMEOUT_MS = 20_000;
const SOCKET_TIMEOUT_MS = 60_000;

class PecError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "PecError";
  }
}

/** PEC settings from the environment, or undefined when PEC isn't set up. */
export function pecConfig(): PecConfig | undefined {
  const address = process.env.PEC_ADDRESS?.trim();
  if (!address) return undefined;
  const port = Number(process.env.PEC_IMAP_PORT?.trim() || "993");
  return {
    address,
    service: process.env.PEC_KEYCHAIN_SERVICE?.trim() || undefined,
    host: process.env.PEC_IMAP_HOST?.trim() || "imaps.pec.aruba.it",
    port: Number.isInteger(port) && port > 0 ? port : 993,
  };
}

function storePasswordHelp(cfg: PecConfig, service = cfg.service ?? "pvault-pec"): string {
  return (
    `ask the user to generate a "password per programmi di posta" in their provider's account panel ` +
    `(Aruba: account.aruba.it/pec, Verifica in 2 passaggi) and store it by running ` +
    (process.platform === "win32"
      ? `\`pvault credential write ${service} --account ${cfg.address}\` in their terminal `
      : `\`security add-generic-password -U -a ${cfg.address} -s ${service} -w\` in their terminal `) +
    `(it prompts for the password; never ask for it in chat). The next call uses it, no restart needed`
  );
}

async function pecPassword(cfg: PecConfig): Promise<string> {
  if (!cfg.service) {
    throw new PecError(`pec: PEC_KEYCHAIN_SERVICE is not set in this MCP server's env — ${storePasswordHelp(cfg)}`);
  }
  if (!KEYCHAIN_SERVICE_PATTERN.test(cfg.service)) {
    throw new PecError(`pec: PEC_KEYCHAIN_SERVICE "${cfg.service}" is not a valid ${SECRET_KIND} name`);
  }
  let password: string;
  try {
    password = await findGenericPassword(cfg.service, cfg.address);
  } catch (e) {
    const code = (e as { code?: number | string }).code;
    if (code === SECURITY_ITEM_NOT_FOUND) {
      throw new PecError(`pec: no password in ${SECRET_KIND} "${cfg.service}" for ${cfg.address} — ${storePasswordHelp(cfg)}`);
    }
    if (code === "ENOENT") {
      throw new PecError(`pec: ${storeToolMissing("PEC_KEYCHAIN_SERVICE")}`);
    }
    throw new PecError(`pec: ${lookupFailure(`${SECRET_STORE} lookup`, e)}`);
  }
  if (!password) {
    throw new PecError(`pec: the ${SECRET_STORE} item "${cfg.service}" is empty — ${storePasswordHelp(cfg)}`);
  }
  return password;
}

/** Connects, runs fn and always logs out. Folders are opened read-only. */
async function withClient<T>(cfg: PecConfig, fn: (client: ImapFlow) => Promise<T>): Promise<T> {
  const client = new ImapFlow({
    host: cfg.host,
    port: cfg.port,
    secure: true,
    auth: { user: cfg.address, pass: await pecPassword(cfg) },
    // The default logger writes to stdout, which is the MCP transport.
    logger: false,
    disableAutoIdle: true,
    connectionTimeout: CONNECT_TIMEOUT_MS,
    socketTimeout: SOCKET_TIMEOUT_MS,
  });
  // Errors after connect arrive as events; without a listener they'd crash the server.
  client.on("error", () => {});
  try {
    await client.connect();
  } catch (e) {
    throw connectError(cfg, e);
  }
  try {
    return await fn(client);
  } finally {
    await client.logout().catch(() => client.close());
  }
}

function connectError(cfg: PecConfig, e: unknown): PecError {
  const err = e as { authenticationFailed?: boolean; code?: string; message?: string };
  if (err.authenticationFailed) {
    return new PecError(
      `pec: login to ${cfg.host} failed for ${cfg.address}. With 2-step verification on, only a ` +
      `"password per programmi di posta" works over IMAP, and Aruba's expire (reportedly after 90 days) — ` +
      storePasswordHelp(cfg),
    );
  }
  return new PecError(`pec: can't connect to ${cfg.host}:${cfg.port} — ${err.code ?? err.message ?? String(e)}`);
}

async function withFolder<T>(cfg: PecConfig, folder: string, fn: (client: ImapFlow) => Promise<T>): Promise<T> {
  return withClient(cfg, async (client) => {
    let lock;
    try {
      lock = await client.getMailboxLock(folder, { readOnly: true });
    } catch (e) {
      const detail = (e as { responseText?: string }).responseText ?? (e as Error).message;
      throw new PecError(`pec: can't open folder "${folder}" — ${detail.replace(/\.+$/, "")}. Use pec_folders to list them`);
    }
    try {
      return await fn(client);
    } finally {
      lock.release();
    }
  });
}

// --- PEC message kinds ---

/**
 * What a message in a PEC mailbox is, from its transport headers:
 * - posta-certificata: a certified message someone sent you (the real one is inside, postacert.eml)
 * - ricevuta-<tipo>: a receipt for a message you sent (accettazione, avvenuta-consegna, errore-consegna, ...)
 * - anomalia: an ordinary or malformed email that reached the PEC box; not certified
 * - email: no PEC headers at all
 */
export function pecKind(headers: { trasporto?: string; ricevuta?: string }): string {
  const ricevuta = headers.ricevuta?.trim().toLowerCase();
  if (ricevuta) return `ricevuta-${ricevuta}`;
  const trasporto = headers.trasporto?.trim().toLowerCase();
  if (trasporto === "posta-certificata") return "posta-certificata";
  if (trasporto === "errore") return "anomalia";
  return trasporto ? `trasporto-${trasporto}` : "email";
}

/** Reads the X-Trasporto / X-Ricevuta lines out of a raw header block. */
export function transportHeaders(raw: string): { trasporto?: string; ricevuta?: string } {
  const unfolded = raw.replace(/\r?\n[ \t]+/g, " ");
  const get = (name: string) => unfolded.match(new RegExp(`^${name}:[ \\t]*(.*)$`, "im"))?.[1]?.trim();
  return { trasporto: get("X-Trasporto"), ricevuta: get("X-Ricevuta") };
}

// --- daticert.xml ---

export interface Certificazione {
  tipo?: string;
  errore?: string;
  mittente?: string;
  destinatari: { indirizzo: string; tipo?: string }[];
  risposte?: string;
  oggetto?: string;
  gestore?: string;
  data?: string;
  identificativo?: string;
  msgid?: string;
  ricevuta?: string;
  consegna?: string;
  errore_esteso?: string;
}

/** A character reference's character, or the reference itself when it isn't a valid code point. */
function charRef(ref: string, code: number): string {
  return code >= 0 && code <= 0x10ffff ? String.fromCodePoint(code) : ref;
}

function xmlText(s: string): string {
  return s
    .replace(/<!\[CDATA\[([\s\S]*?)\]\]>/g, "$1")
    .replace(/&#x([0-9a-f]+);/gi, (ref, h) => charRef(ref, parseInt(h, 16)))
    .replace(/&#(\d+);/g, (ref, d) => charRef(ref, parseInt(d, 10)))
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&apos;/g, "'")
    .replace(/&amp;/g, "&")
    .trim();
}

function xmlAttr(attrs: string, name: string): string | undefined {
  const m = attrs.match(new RegExp(`\\b${name}\\s*=\\s*("([^"]*)"|'([^']*)')`));
  return m ? xmlText(m[2] ?? m[3] ?? "") : undefined;
}

/** Parses the provider's certification data (daticert.xml, DM 2 novembre 2005). */
export function parseDaticert(xml: string): Certificazione {
  const one = (tag: string) => {
    const m = xml.match(new RegExp(`<${tag}(?:\\s[^>]*)?>([\\s\\S]*?)</${tag}>`));
    return m ? xmlText(m[1]) || undefined : undefined;
  };
  const attrsOf = (tag: string) => xml.match(new RegExp(`<${tag}(\\s[^>]*?)?/?>`))?.[1] ?? "";

  const root = attrsOf("postacert");
  const destinatari = [...xml.matchAll(/<destinatari(\s[^>]*)?>([\s\S]*?)<\/destinatari>/g)].map((m) => ({
    indirizzo: xmlText(m[2]),
    tipo: xmlAttr(m[1] ?? "", "tipo"),
  }));

  let data: string | undefined;
  const giorno = one("giorno")?.match(/^(\d{1,2})\/(\d{1,2})\/(\d{4})$/);
  const ora = one("ora");
  if (giorno && ora && /^\d{2}:\d{2}:\d{2}$/.test(ora)) {
    const zona = xmlAttr(attrsOf("data"), "zona");
    const offset = zona && /^[+-]\d{4}$/.test(zona) ? `${zona.slice(0, 3)}:${zona.slice(3)}` : "";
    data = `${giorno[3]}-${giorno[2].padStart(2, "0")}-${giorno[1].padStart(2, "0")}T${ora}${offset}`;
  }

  return {
    tipo: xmlAttr(root, "tipo"),
    errore: xmlAttr(root, "errore"),
    mittente: one("mittente"),
    destinatari,
    risposte: one("risposte"),
    oggetto: one("oggetto"),
    gestore: one("gestore-emittente"),
    data,
    identificativo: one("identificativo"),
    msgid: one("msgid"),
    ricevuta: xmlAttr(attrsOf("ricevuta"), "tipo"),
    consegna: one("consegna"),
    errore_esteso: one("errore-esteso"),
  };
}

// --- Parsing a whole message ---

export interface AttachmentInfo {
  index: number;
  filename?: string;
  content_type: string;
  size: number;
}

export interface ParsedPec {
  kind: string;
  /** The provider's certification data; the trustworthy record of who sent what, when. */
  certificazione?: Certificazione;
  /** The certified message itself (postacert.eml), or the message as received when there is none. */
  message: {
    from: string[];
    to: string[];
    cc: string[];
    subject?: string;
    date?: string;
    text: string;
    attachments: AttachmentInfo[];
  };
  /** The envelope the provider wrapped it in, when the message is a PEC transport; text is its notice (e.g. why delivery failed). */
  envelope?: { from: string[]; subject?: string; date?: string; text: string };
}

function addresses(a: AddressObject | AddressObject[] | undefined): string[] {
  const list = a === undefined ? [] : Array.isArray(a) ? a : [a];
  return list.flatMap((o) => o.value).map((v) => (v.name ? `${v.name} <${v.address ?? ""}>` : v.address ?? "")).filter(Boolean);
}

function headerValue(mail: ParsedMail, name: string): string | undefined {
  const v = mail.headers.get(name);
  return typeof v === "string" ? v : undefined;
}

/** Attachments that are the message's own files, not the PEC wrapping. */
function contentAttachments(mail: ParsedMail, isEnvelope: boolean): Attachment[] {
  if (!isEnvelope) return mail.attachments;
  return mail.attachments.filter(
    (a) => !(a.contentType === "message/rfc822" || /^(daticert\.xml|smime\.p7s)$/i.test(a.filename ?? "")),
  );
}

/** Decodes bytes in the given charset, falling back to UTF-8 for unknown labels. */
export function decodeText(data: Buffer, charset: string | undefined): string {
  try {
    return new TextDecoder(charset || "utf-8").decode(data);
  } catch {
    return new TextDecoder("utf-8").decode(data);
  }
}

/** Decodes an XML document using the encoding its declaration names (UTF-8 by default). */
export function decodeXml(data: Buffer): string {
  const declared = data.subarray(0, 200).toString("latin1").match(/^\s*<\?xml[^>]*\bencoding\s*=\s*["']([^"']+)["']/i)?.[1];
  return decodeText(data, declared);
}

/** The charset parameter of an attachment's Content-Type, if any. */
export function attachmentCharset(a: Attachment): string | undefined {
  const ct = a.headers.get("content-type") as { params?: Record<string, string> } | string | undefined;
  return typeof ct === "object" ? ct?.params?.charset : undefined;
}

interface Parts {
  outer: ParsedMail;
  inner?: ParsedMail;
  kind: string;
  certificazione?: Certificazione;
}

/**
 * Keeps attached messages (postacert.eml) as message/rfc822 attachments.
 * By default mailparser splices inline ones into the parent's body and
 * attachments, mixing the envelope with the certified message.
 */
const PARSE_OPTIONS = { ignoreEmbedded: true } as Parameters<typeof simpleParser>[1];

async function parseParts(source: Buffer): Promise<Parts> {
  const outer = await simpleParser(source, PARSE_OPTIONS);
  const kind = pecKind({
    trasporto: headerValue(outer, "x-trasporto"),
    ricevuta: headerValue(outer, "x-ricevuta"),
  });
  // Only a PEC transport wraps a certified message; in ordinary mail (e.g. the
  // sent copy of a PEC) an attached .eml is just an attachment.
  if (kind === "email") return { outer, kind };
  const daticert = outer.attachments.find((a) => /^daticert\.xml$/i.test(a.filename ?? ""));
  const rfc822 = outer.attachments.filter((a) => a.contentType === "message/rfc822");
  const postacert = rfc822.find((a) => /^postacert\.eml$/i.test(a.filename ?? "")) ?? rfc822[0];
  return {
    outer,
    inner: postacert ? await simpleParser(postacert.content, PARSE_OPTIONS) : undefined,
    kind,
    certificazione: daticert ? parseDaticert(decodeXml(daticert.content)) : undefined,
  };
}

function attachmentList(parts: Parts): Attachment[] {
  return parts.inner ? parts.inner.attachments : contentAttachments(parts.outer, parts.kind !== "email");
}

function bodyText(mail: ParsedMail): string {
  const text = (mail.text ?? "").trim();
  if (text.length <= MAX_TEXT_CHARS) return text;
  return `${text.slice(0, MAX_TEXT_CHARS)}\n\n[… cut: ${text.length - MAX_TEXT_CHARS} more characters]`;
}

/** Parses a raw message from a PEC mailbox into what an agent needs. */
export async function parsePec(source: Buffer): Promise<ParsedPec> {
  const parts = await parseParts(source);
  const { outer, inner } = parts;
  const shown = inner ?? outer;
  return {
    kind: parts.kind,
    certificazione: parts.certificazione,
    message: {
      from: addresses(shown.from),
      to: addresses(shown.to),
      cc: addresses(shown.cc),
      subject: shown.subject,
      date: shown.date?.toISOString(),
      text: bodyText(shown),
      attachments: attachmentList(parts).map((a, index) => ({
        index,
        filename: a.filename,
        content_type: a.contentType,
        size: a.size,
      })),
    },
    envelope: parts.kind === "email" ? undefined : {
      from: addresses(outer.from),
      subject: outer.subject,
      date: outer.date?.toISOString(),
      text: bodyText(outer),
    },
  };
}

/** One attachment of a raw message, by the index parsePec listed. */
export async function pecAttachment(source: Buffer, index: number): Promise<Attachment> {
  const list = attachmentList(await parseParts(source));
  const a = list[index];
  if (!a) throw new PecError(`pec: no attachment ${index}; this message has ${list.length}`);
  return a;
}

// --- Mailbox operations ---

export interface MessageSummary {
  uid: number;
  date?: string;
  kind: string;
  /** For certified messages, the original sender (from Reply-To or "Per conto di:"). */
  sender?: string;
  subject?: string;
  seen: boolean;
  size?: number;
}

/**
 * The original sender of a PEC envelope, whose From is "Per conto di: x" <posta-certificata@...>.
 * Only the provider's envelope is unwrapped, so a message must carry PEC transport headers
 * (kind isn't "email"): any other message names its own From address, since its display
 * name, Reply-To and even a posta-certificata@ address at some other domain are whatever
 * the sender chose.
 */
export function envelopeSender(
  from: { name?: string; address?: string }[] | undefined,
  replyTo: { name?: string; address?: string }[] | undefined,
  kind: string,
): string | undefined {
  const address = from?.[0]?.address;
  if (kind === "email" || !/^posta-certificata@/i.test(address ?? "")) return address;
  const perConto = from?.[0]?.name?.match(/^Per conto di:\s*(\S+@\S+)/i)?.[1];
  return perConto ?? replyTo?.[0]?.address ?? address;
}

function summarize(msg: FetchMessageObject): MessageSummary {
  const internal = msg.internalDate ? new Date(msg.internalDate) : undefined;
  const kind = pecKind(transportHeaders(msg.headers?.toString("utf8") ?? ""));
  return {
    uid: msg.uid,
    date: internal && !isNaN(internal.getTime()) ? internal.toISOString() : undefined,
    kind,
    sender: envelopeSender(msg.envelope?.from, msg.envelope?.replyTo, kind),
    subject: msg.envelope?.subject,
    seen: msg.flags?.has("\\Seen") ?? false,
    size: msg.size,
  };
}

/** True for a YYYY-MM-DD string naming a real day (no 2026-02-30). */
export function isCalendarDate(s: string): boolean {
  const m = s.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  if (!m) return false;
  const d = new Date(Date.UTC(+m[1], +m[2] - 1, +m[3]));
  return d.getUTCFullYear() === +m[1] && d.getUTCMonth() === +m[2] - 1 && d.getUTCDate() === +m[3];
}

export interface ListOptions {
  folder: string;
  limit: number;
  from?: string;
  subject?: string;
  text?: string;
  since?: string;
  before?: string;
  unseen?: boolean;
}

/** Newest-first summaries of the messages matching the filters. */
export async function listMessages(cfg: PecConfig, opts: ListOptions): Promise<{ total: number; messages: MessageSummary[] }> {
  for (const d of [opts.since, opts.before]) {
    if (d !== undefined && !isCalendarDate(d)) throw new PecError(`pec: "${d}" is not a real date (YYYY-MM-DD)`);
  }
  const query: SearchObject = {};
  if (opts.from) query.from = opts.from;
  if (opts.subject) query.subject = opts.subject;
  if (opts.text) query.text = opts.text;
  if (opts.since) query.since = new Date(`${opts.since}T00:00:00Z`);
  if (opts.before) query.before = new Date(`${opts.before}T00:00:00Z`);
  if (opts.unseen) query.seen = false;
  if (Object.keys(query).length === 0) query.all = true;

  return withFolder(cfg, opts.folder, async (client) => {
    // imapflow returns false when the server rejects the search.
    const found = await client.search(query, { uid: true });
    if (found === false || found === undefined) {
      throw new PecError(`pec: the server rejected the search in "${opts.folder}"`);
    }
    const uids = found;
    const newest = [...uids].sort((a, b) => b - a).slice(0, opts.limit);
    const messages: MessageSummary[] = [];
    if (newest.length > 0) {
      for await (const msg of client.fetch(
        newest,
        { uid: true, envelope: true, flags: true, internalDate: true, size: true, headers: ["x-trasporto", "x-ricevuta"] },
        { uid: true },
      )) {
        messages.push(summarize(msg));
      }
    }
    messages.sort((a, b) => b.uid - a.uid);
    return { total: uids.length, messages };
  });
}

/** Downloads one message without marking it read. */
export async function fetchSource(cfg: PecConfig, folder: string, uid: number): Promise<{ source: Buffer; seen: boolean }> {
  return withFolder(cfg, folder, async (client) => {
    const meta = await client.fetchOne(String(uid), { uid: true, size: true, flags: true }, { uid: true });
    if (!meta) throw new PecError(`pec: no message with uid ${uid} in ${folder}`);
    if ((meta.size ?? 0) > MAX_MESSAGE_BYTES) {
      throw new PecError(`pec: message ${uid} is ${Math.round((meta.size ?? 0) / 1048576)} MB, over the ${MAX_MESSAGE_BYTES / 1048576} MB limit`);
    }
    // BODY.PEEK[] in a read-only (EXAMINE) folder: the \Seen flag is left alone.
    const msg = await client.fetchOne(String(uid), { uid: true, source: true }, { uid: true });
    if (!msg || !msg.source) throw new PecError(`pec: message ${uid} has no content`);
    return { source: msg.source, seen: meta.flags?.has("\\Seen") ?? false };
  });
}

export async function listFolders(cfg: PecConfig): Promise<{ path: string; name: string; special_use?: string }[]> {
  return withClient(cfg, async (client) =>
    (await client.list()).map((f) => ({ path: f.path, name: f.name, special_use: f.specialUse })),
  );
}
