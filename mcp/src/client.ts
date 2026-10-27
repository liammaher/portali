import { exec } from "node:child_process";
import { lstat, readFile, unlink } from "node:fs/promises";
import { request } from "node:http";
import { homedir, userInfo } from "node:os";
import { dirname, join } from "node:path";
import { promisify } from "node:util";
import {
  findGenericPassword, KEYCHAIN_SERVICE_PATTERN, LOOKUP_MAX_BYTES, LOOKUP_TIMEOUT_MS,
  SECURITY_ITEM_NOT_FOUND, SECURITY_PATH,
} from "./keychain.js";

const execAsync = promisify(exec);

interface VaultStatus {
  initialized: boolean;
  locked: boolean;
  field_count: number;
  categories: Record<string, number>;
}

interface FieldInfo {
  id: string;
  category: string;
  field_name: string;
  value?: string;
  sensitivity: string;
  updated_at: string;
  version: number;
  /** Computed by the vault from an address (municipalità of an address in Napoli), not stored. */
  derived?: boolean;
}

interface ContextBundle {
  categories: Record<string, FieldInfo[]>;
}

class VaultError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "VaultError";
  }
}

/** A person's slug, as vault.ValidPersonSlug has it ("io" is the owner). */
export const SLUG = "[a-z][a-z0-9-]{0,31}";

/** A vault name: a slug, as `--vault <name>` takes it. */
const VAULT_NAME = new RegExp(`^(?!io$)${SLUG}$`);

/** The vault's folder: VAULT_DIR, else ~/.pvault-<VAULT_NAME>, else ~/.pvault. */
function vaultDir(): string {
  const dir = process.env.VAULT_DIR?.trim();
  if (dir) return dir;
  const name = process.env.VAULT_NAME?.trim();
  if (name) {
    if (!VAULT_NAME.test(name)) {
      throw new VaultError(`vault: VAULT_NAME ${JSON.stringify(name)} isn't a vault name (lower-case letters, digits and hyphens)`);
    }
    return join(homedir(), `.pvault-${name}`);
  }
  return join(homedir(), ".pvault");
}

const REQUEST_TIMEOUT_MS = 30_000;

function notRunning(): VaultError {
  return new VaultError(`vault: server not running — run '${pvCmd("unlock")}'`);
}

const PIPE_NAME = /^\\\\\.\\pipe\\pvault-[0-9a-f]{32}$/;

/**
 * The named pipe of this vault's Windows server, from the file it writes in
 * <vault dir>\run, a folder only this user can enter. Pipe names are
 * machine-wide and any account can create one, so a name is trusted only
 * while the server still holds that file open: Windows won't delete a file
 * another process holds open, so if the delete works the server is gone and
 * the name may be someone else's by now.
 */
export async function serverPipe(
  file: string,
  fs: { readFile: (f: string) => Promise<string>; unlink: (f: string) => Promise<void> } = {
    readFile: (f) => readFile(f, "ascii"), unlink,
  },
): Promise<string> {
  const name = (await fs.readFile(file).catch(() => { throw notRunning(); })).trim();
  try {
    await fs.unlink(file);
  } catch (e) {
    const code = (e as NodeJS.ErrnoException).code;
    if (code === "ENOENT") throw notRunning();
    if (code !== "EBUSY") throw new VaultError(`vault: refusing to send the token: can't tell whether ${file} is the server's (${code})`);
    if (!PIPE_NAME.test(name)) throw new VaultError(`vault: refusing to send the token: ${file} doesn't name a vault server's pipe`);
    return name;
  }
  throw notRunning();
}

/**
 * Requests, the token included, go only to the server's socket in the vault
 * folder, never over TCP: anything can listen on 127.0.0.1:7200 while the
 * vault is locked, but only this user can put a socket in <vault dir>/run,
 * which the server keeps private (0700). On Windows the server listens on a
 * named pipe instead (serverPipe).
 */
async function checkSocket(path: string): Promise<void> {
  const uid = process.getuid?.();
  const dir = await lstat(dirname(path)).catch(() => undefined);
  if (!dir) throw notRunning();
  if (!dir.isDirectory() || dir.uid !== uid || (dir.mode & 0o077) !== 0) {
    throw new VaultError(`vault: refusing to send the token: ${dirname(path)} must be a private folder of yours (mode 0700)`);
  }
  const sock = await lstat(path).catch(() => undefined);
  if (!sock) throw notRunning();
  if (!sock.isSocket() || sock.uid !== uid) {
    throw new VaultError(`vault: refusing to send the token: ${path} is not your vault server's socket`);
  }
}

interface SocketInit {
  method?: string;
  headers?: Record<string, string>;
  body?: string | Uint8Array;
}

/** One HTTP request over the socket, answered as a fetch Response. */
function socketRequest(socketPath: string, path: string, init: SocketInit): Promise<Response> {
  return new Promise((resolve, reject) => {
    const req = request({
      socketPath, path, method: init.method ?? "GET",
      // The server wants a loopback Host on every request.
      headers: {
        ...init.headers, Host: "localhost",
        ...(init.body !== undefined && { "Content-Length": String(Buffer.byteLength(init.body)) }),
      },
      timeout: REQUEST_TIMEOUT_MS,
    }, (res) => {
      const chunks: Buffer[] = [];
      res.on("data", (c: Buffer) => chunks.push(c));
      res.on("error", reject);
      res.on("end", () => {
        const headers = new Headers();
        for (const [k, v] of Object.entries(res.headers)) {
          if (v !== undefined) headers.set(k, Array.isArray(v) ? v.join(", ") : v);
        }
        const status = res.statusCode ?? 500;
        const body = status === 204 || status === 304 ? null : Buffer.concat(chunks);
        resolve(new Response(body, { status, headers }));
      });
    });
    req.on("timeout", () => req.destroy(new Error("timed out")));
    req.on("error", reject);
    req.end(init.body);
  });
}

/**
 * Sends a request to this vault's server over its socket. With VAULT_ADDR
 * set, the server there must be the one it names (pvault status prints it),
 * so an MCP server set up for one vault never answers from another's.
 */
async function vaultFetch(path: string, init: SocketInit): Promise<Response> {
  let sock: string;
  if (process.platform === "win32") {
    sock = await serverPipe(join(vaultDir(), "run", "pipe"));
  } else {
    sock = join(vaultDir(), "run", "pvault.sock");
    await checkSocket(sock);
  }
  const send = async (p: string, i: SocketInit) => {
    try {
      return await socketRequest(sock, p, i);
    } catch {
      throw notRunning();
    }
  };
  const addr = process.env.VAULT_ADDR?.trim();
  if (addr) {
    let want = "";
    try {
      want = new URL(addr).host;
    } catch {
      throw new VaultError(`vault: VAULT_ADDR ${addr} isn't an address like http://127.0.0.1:7200`);
    }
    const status = await send("/vault/status", {});
    const got = status.ok ? ((await status.json().catch(() => ({}))) as { addr?: string }).addr : undefined;
    if (got !== want) {
      throw new VaultError(`vault: VAULT_ADDR ${addr} isn't the server of ${vaultDir()}, which listens on ${got ?? "an unknown address"} — set VAULT_NAME (or VAULT_DIR) to the vault you mean, and VAULT_ADDR to what \`${pvCmd("status")}\` prints, or unset it`);
    }
  }
  return send(path, init);
}

const TOKEN_LOOKUP_TIMEOUT_MS = LOOKUP_TIMEOUT_MS;
const TOKEN_LOOKUP_MAX_BYTES = LOOKUP_MAX_BYTES;

/** Printable ASCII without spaces: anything else can't go in a Bearer header. */
const TOKEN_PATTERN = /^[\x21-\x7e]+$/;

function staticToken(): string | undefined {
  return process.env.VAULT_TOKEN?.trim() || undefined;
}

function keychainService(): string | undefined {
  return process.env.VAULT_KEYCHAIN_SERVICE?.trim() || undefined;
}

function tokenCommand(): string | undefined {
  return process.env.VAULT_TOKEN_COMMAND?.trim() || undefined;
}

/**
 * A pvault command line for messages. VAULT_NAME names a vault other than the
 * default one (pvault --vault <name>), e.g. a business vault.
 */
export function pvCmd(args: string): string {
  const name = process.env.VAULT_NAME;
  return name ? `pvault --vault ${name} ${args}` : `pvault ${args}`;
}

/**
 * How to get on without a working token: Touch ID first, since with it on the user
 * approves `pvault get`/`pvault set` from the shell and no token is needed; else a
 * new token, matching whichever source is actually used.
 */
function createTokenHelp(): string {
  const touchID = `if \`${pvCmd("touchid status")}\` says on, use \`${pvCmd("get <id> ...")}\` or \`${pvCmd("set")}\` ` +
    "from the shell instead (the user approves with Touch ID, no token needed); otherwise ";
  const create = `\`${pvCmd("create-service-token")} <name> --scope "identity.*,..."`;
  const noRestart = "in their terminal; the next call uses it, no restart needed";
  let ask = `ask the user to create one with ${create}\` and set it as VAULT_TOKEN in this MCP server's env`;
  if (!staticToken()) {
    const service = keychainService();
    if (service) {
      ask = `ask the user to run ${create} --keychain ${service}\` ${noRestart}`;
    } else if (tokenCommand()) {
      ask = `ask the user to run ${create} --keychain <service>\` (the service VAULT_TOKEN_COMMAND reads) ${noRestart}`;
    }
  }
  return touchID + ask;
}

function missingToken(detail?: string): VaultError {
  const why = detail ? ` (${detail})` : "";
  return new VaultError(`vault: no service token${why} — ${createTokenHelp()}`);
}

/** Why a token or password lookup process failed, in one line. */
export function lookupFailure(label: string, e: unknown): string {
  const err = e as { killed?: boolean; signal?: string; code?: number | string; stderr?: string };
  if (err.code === "ERR_CHILD_PROCESS_STDIO_MAXBUFFER") {
    return `${label} printed more than ${TOKEN_LOOKUP_MAX_BYTES / 1024} KB, which can't be a token or password`;
  }
  if (err.killed || err.signal) {
    return `${label} timed out after ${TOKEN_LOOKUP_TIMEOUT_MS / 1000}s; a Keychain prompt may be waiting`;
  }
  const line = err.stderr?.trim().split("\n")[0];
  return `${label} failed: ${line || `exit ${err.code ?? "unknown"}`}`;
}

/** Reads the token pvault create-service-token --keychain stored for this user. */
async function keychainToken(service: string): Promise<string> {
  if (!KEYCHAIN_SERVICE_PATTERN.test(service)) {
    throw new VaultError(`vault: VAULT_KEYCHAIN_SERVICE "${service}" is not a valid Keychain service name`);
  }
  try {
    return await findGenericPassword(service, userInfo().username);
  } catch (e) {
    const code = (e as { code?: number | string }).code;
    if (code === SECURITY_ITEM_NOT_FOUND) {
      throw missingToken(`nothing stored in Keychain service "${service}"`);
    }
    if (code === "ENOENT") {
      throw missingToken(`${SECURITY_PATH} not found; VAULT_KEYCHAIN_SERVICE needs macOS`);
    }
    throw missingToken(lookupFailure("Keychain lookup", e));
  }
}

/** Runs VAULT_TOKEN_COMMAND in the platform shell and returns its output. */
async function commandToken(command: string): Promise<string> {
  try {
    const { stdout } = await execAsync(command, {
      timeout: TOKEN_LOOKUP_TIMEOUT_MS,
      maxBuffer: TOKEN_LOOKUP_MAX_BYTES,
    });
    return stdout.trim();
  } catch (e) {
    throw missingToken(lookupFailure("VAULT_TOKEN_COMMAND", e));
  }
}

/**
 * The MCP server only ever uses a scoped service token supplied by the user:
 * VAULT_TOKEN, else the Keychain item named by VAULT_KEYCHAIN_SERVICE, else
 * the output of VAULT_TOKEN_COMMAND. It never reads ~/.pvault/.session: that
 * token has full, uncapped access and would bypass the scope and sensitivity
 * limits.
 *
 * Lookups run for every request rather than being cached, so a token the
 * user replaces mid-session is used right away.
 */
async function resolveToken(): Promise<string> {
  let token = staticToken();
  let source = "VAULT_TOKEN";
  if (!token) {
    const service = keychainService();
    const command = tokenCommand();
    if (service) {
      source = `Keychain service "${service}"`;
      token = await keychainToken(service);
    } else if (command) {
      source = "VAULT_TOKEN_COMMAND output";
      token = await commandToken(command);
    } else {
      throw missingToken();
    }
    if (!token) {
      throw missingToken(`${source} is empty`);
    }
  }
  if (!TOKEN_PATTERN.test(token)) {
    throw new VaultError(`vault: ${source} is not a valid token (it contains spaces, line breaks or non-ASCII characters)`);
  }
  return token;
}

/**
 * Fails fast when no token source is configured at all. With a Keychain
 * service or command the server starts even before a token is stored, so
 * the user can create one mid-session.
 */
export function assertTokenConfigured(): void {
  if (!staticToken() && !keychainService() && !tokenCommand()) {
    throw missingToken();
  }
}

async function authedFetch(path: string, init: SocketInit): Promise<Response> {
  // First: a misconfigured vault must say so, not read as a missing token.
  vaultDir();
  const headers = { ...init.headers, Authorization: `Bearer ${await resolveToken()}` };
  const resp = await vaultFetch(path, { ...init, headers });
  if (!resp.ok) {
    throw mapError(resp.status, await resp.text());
  }
  return resp;
}

function mapError(status: number, body: string): VaultError {
  if (status === 401) {
    return new VaultError(
      `vault: service token invalid, expired or revoked — ${createTokenHelp()}`
    );
  }
  if (status === 403) {
    let constraint = "";
    try {
      constraint = JSON.parse(body).constraint ?? "";
    } catch {
      // not JSON
    }
    if (constraint === "vault_locked") {
      return new VaultError(`vault: vault is locked — ask the user to run '${pvCmd("unlock")}'`);
    }
    if (constraint === "sensitivity_exceeded") {
      return new VaultError(
        "vault: this item's sensitivity tier is above what this token may access"
      );
    }
    if (constraint === "read_only") {
      return new VaultError(
        "vault: this token is read-only — ask the user for a token created with --access append (to add) or --access write (to change or delete)"
      );
    }
    if (constraint === "append_only") {
      return new VaultError(
        "vault: this token can add new items but not overwrite or delete — that needs a token created with --access write"
      );
    }
    if (constraint === "scope_exceeded") {
      return new VaultError("vault: this is outside the token's scope");
    }
    if (constraint === "personal_only") {
      return new VaultError(
        "vault: personal items (login secrets such as SPID/CIE credentials or a PIN) can only be changed by the user — send them to the vault UI"
      );
    }
    if (constraint === "session_required") {
      return new VaultError(
        "vault: only the user's own session can do this (lower a sensitivity tier, or delete an item stored above its default tier) — ask the user to do it in the vault UI or CLI"
      );
    }
    return new VaultError(`vault: forbidden — ${body}`);
  }
  if (status === 404) {
    return new VaultError(`vault: not found — ${body}`);
  }

  // Try to extract error message from JSON response
  try {
    const parsed = JSON.parse(body);
    if (parsed.error) {
      return new VaultError(`vault: ${parsed.error}`);
    }
  } catch {
    // not JSON
  }

  return new VaultError(`vault: HTTP ${status} — ${body}`);
}

async function apiRequest(
  method: string,
  path: string,
  body?: unknown
): Promise<unknown> {
  const resp = await authedFetch(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  return resp.json();
}

export async function getStatus(): Promise<VaultStatus> {
  // Public endpoint — works without auth
  const resp = await vaultFetch("/vault/status", {});
  if (!resp.ok) {
    throw new VaultError(`vault: status check failed — HTTP ${resp.status}`);
  }
  return resp.json() as Promise<VaultStatus>;
}

export async function getSchema(): Promise<unknown> {
  // Public endpoint — works without auth
  const resp = await vaultFetch("/vault/schema", {});
  if (!resp.ok) {
    throw new VaultError(`vault: schema fetch failed — HTTP ${resp.status}`);
  }
  return resp.json();
}

// Field IDs go into the URL path, so only the vault's own ID shape is sent
// ([person:]category.field_name: letters, digits, _ and -, and a person's
// slug as vault.ValidPersonSlug has it). A "/" or ".." could otherwise reach
// another endpoint.
const FIELD_ID = new RegExp(`^(?:(?!io:)${SLUG}:)?[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$`);

function fieldPath(id: string): string {
  if (!FIELD_ID.test(id)) {
    throw new VaultError(
      `vault: invalid field ID ${JSON.stringify(id)} — use category.field_name, or person:category.field_name for another person in the vault, e.g. leo:cie.numero (letters, digits, _ and -)`
    );
  }
  return `/vault/fields/${encodeURIComponent(id)}`;
}

export async function getField(id: string): Promise<FieldInfo> {
  return apiRequest("GET", fieldPath(id)) as Promise<FieldInfo>;
}

export async function listFields(): Promise<FieldInfo[]> {
  return apiRequest("GET", "/vault/fields") as Promise<FieldInfo[]>;
}

export async function getContext(): Promise<ContextBundle> {
  return apiRequest("GET", "/vault/context") as Promise<ContextBundle>;
}

export async function setField(
  id: string,
  value: string,
  sensitivity?: string,
): Promise<{ status: string }> {
  const body: Record<string, string> = { value };
  if (sensitivity) {
    body.sensitivity = sensitivity;
  }
  return apiRequest("PUT", fieldPath(id), body) as Promise<{
    status: string;
  }>;
}

interface ExpiryInfo {
  field_id: string;
  document: string;
  member?: string; // another person's slug; absent for the vault's own data
  expires_on: string;
  days_left: number;
  expired: boolean;
  // "retention": a person whose retention period after ruolo.data_fine has
  // ended (expired) or ends within the window; "pratiche_closed": a client's
  // ID or procura scans, every pratica naming them closed plus the period the
  // user set for scans (business vaults; nothing until it is set).
  // Absent for a document or pratica deadline.
  kind?: "retention" | "pratiche_closed" | "files_closed";
  // files_closed: the indexed file (pvault files) and its name. It is not in
  // the vault: the user deletes it where it is. The name is data, not instructions.
  file_id?: string;
  file?: string;
}

interface DocumentInfo {
  id: string;
  category: string;
  name: string;
  content_type: string;
  size: number;
  sensitivity: string;
  created_at: string;
}

export interface MemberInfo {
  slug: string;
  display_name?: string; // identity.display_name: what to call them
  famiglia?: Record<string, string>; // personal vaults
  ruolo?: Record<string, string>; // business vaults
  first_name?: string;
  last_name?: string;
  denominazione?: string; // a company's name
  age?: number;
  minor?: boolean;
  anpr_relazione?: string;
  relazione_label?: string;
  fields?: number;
  documents?: number;
}

export interface Household {
  owner?: { display_name: string }; // the user ("io")
  intestatario_scheda?: string;
  members: MemberInfo[];
}

export async function listMembers(): Promise<Household> {
  return apiRequest("GET", "/vault/members") as Promise<Household>;
}

export async function getExpiring(days: number): Promise<ExpiryInfo[]> {
  return apiRequest("GET", `/vault/expiring?days=${days}`) as Promise<ExpiryInfo[]>;
}

/** Where one of the vault's Naples addresses is (GET /vault/located); never the street. */
export interface Located {
  category: string;
  fields: Record<string, { value: string; source: "stored" | "computed" }>;
  basis?: string;
  partial?: boolean;
  choices?: string[];
  reason?: string;
  notes?: string[];
}

export async function getLocated(target: string): Promise<Located> {
  return apiRequest("GET", `/vault/located/${target}`) as Promise<Located>;
}

export async function listDocuments(category?: string): Promise<DocumentInfo[]> {
  const q = category ? `?category=${encodeURIComponent(category)}` : "";
  return apiRequest("GET", `/vault/documents${q}`) as Promise<DocumentInfo[]>;
}

/** Upload raw bytes as an encrypted document. */
export async function addDocument(
  category: string,
  name: string,
  contentType: string,
  data: Uint8Array,
  sensitivity?: string,
): Promise<DocumentInfo> {
  const q = new URLSearchParams({ category, name });
  if (sensitivity) q.set("sensitivity", sensitivity);
  const resp = await authedFetch(`/vault/documents?${q}`, {
    method: "POST",
    headers: { "Content-Type": contentType },
    body: data,
  });
  return (await resp.json()) as DocumentInfo;
}

/** Download and decrypt a document. */
export async function getDocument(
  id: string,
): Promise<{ contentType: string; category: string; data: Buffer }> {
  const resp = await authedFetch(`/vault/documents/${encodeURIComponent(id)}`, { method: "GET" });
  return {
    contentType: resp.headers.get("Content-Type") ?? "application/octet-stream",
    category: resp.headers.get("X-Document-Category") ?? "",
    data: Buffer.from(await resp.arrayBuffer()),
  };
}

// --- file index (pvault files) ---

export interface FileLocation {
  source: string;
  key: string;
  path?: string;
  web_url?: string;
}

export interface FileLinkInfo {
  id: string;
  holder: string; // "" = the owner or the business
  kind: "person" | "client" | "business" | "pratica";
  pratica?: string;
  origin: "manual" | "rule" | "suggestion";
  role?: string;
  note?: string;
}

export interface IndexedFile {
  id: string;
  source_id: string;
  tier: string;
  gone?: boolean;
  name: string;
  locations: FileLocation[];
  mime?: string;
  size?: number;
  mtime?: string;
  links: FileLinkInfo[];
}

export interface FileSource {
  id: string;
  provider: string;
  label: string;
  root?: string;
  via?: string; // "connector": files an agent found through its own connector
}

export interface FileSearch {
  text?: string;
  for?: string; // a person's slug, or io
  pratica?: string;
  unlinked?: boolean;
  gone?: boolean;
  provider?: string;
  limit?: number;
}

export async function searchFiles(q: FileSearch): Promise<{ files: IndexedFile[]; total: number }> {
  const p = new URLSearchParams();
  if (q.text) p.set("q", q.text);
  if (q.for) p.set("for", q.for);
  if (q.pratica) p.set("pratica", q.pratica);
  if (q.provider) p.set("provider", q.provider);
  if (q.unlinked) p.set("unlinked", "1");
  if (q.gone) p.set("gone", "1");
  if (q.limit) p.set("limit", String(q.limit));
  return apiRequest("GET", `/vault/files?${p}`) as Promise<{ files: IndexedFile[]; total: number }>;
}

// A file's ID is the vault's own 32 hex characters; nothing else goes into a URL path.
const FILE_ID = /^[0-9a-f]{32}$/;

export function checkFileId(id: string): string {
  if (!FILE_ID.test(id)) {
    throw new VaultError("vault: a file id is the 32 hex characters vault_files_search shows");
  }
  return id;
}

export async function getIndexedFile(id: string): Promise<IndexedFile> {
  return apiRequest("GET", `/vault/files/${checkFileId(id)}`) as Promise<IndexedFile>;
}

/** The places files are read from. A token that can't see them gets none. */
export async function listFileSources(): Promise<FileSource[]> {
  try {
    return (await apiRequest("GET", "/vault/files/sources")) as FileSource[];
  } catch (e) {
    if (e instanceof VaultError && /outside the token's scope|sensitivity tier|forbidden/.test(e.message)) return [];
    throw e;
  }
}

export interface FileSuggestion {
  for: string;
  kind?: string;
  pratica?: string;
  reason?: string;
}

export async function suggestFileLink(id: string, s: FileSuggestion): Promise<{ id: string }> {
  return apiRequest("POST", `/vault/files/${checkFileId(id)}/suggest`, s) as Promise<{ id: string }>;
}

export interface FileProposal {
  provider: "gdrive" | "onedrive" | "sharepoint" | "dropbox";
  key: string; // the provider's own id for the file
  name: string;
  path?: string;
  web_url?: string;
  mime?: string;
  size?: number;
  mtime?: string;
  provider_hash?: string;
  for?: string;
  kind?: string;
  pratica?: string;
  role?: string;
  reason?: string;
}

/** Propose a file the agent's own connector found. It joins the index only when the user accepts. */
export async function proposeFile(p: FileProposal): Promise<{ id: string }> {
  return apiRequest("POST", "/vault/files/proposals", p) as Promise<{ id: string }>;
}

export type { VaultStatus, FieldInfo, ContextBundle, ExpiryInfo, DocumentInfo };

export interface SigningPlacement {
  page: number;
  rect: [number, number, number, number];
  date_rect?: [number, number, number, number];
}

export interface SigningInfo {
  id: string;
  name: string;
  purpose?: string;
  requester: string;
  signer: string;
  status: "pending" | "approved" | "rejected" | "expired";
  pages: number;
  date: string;
  expires_at: string;
}

/**
 * File a request to sign a PDF. The vault stamps the user's signature and
 * holds the result: nothing is released until the user approves it in person
 * (`pvault sign review`). There is deliberately no way to approve from here.
 */
export async function requestSigning(
  name: string,
  purpose: string,
  pdf: Uint8Array,
  placements: SigningPlacement[],
): Promise<SigningInfo> {
  return (await apiRequest("POST", "/vault/signing", {
    name, purpose, pdf: Buffer.from(pdf).toString("base64"), placements,
  })) as SigningInfo;
}

/** This token's signing requests, or one of them. */
export async function getSignings(id?: string): Promise<SigningInfo[]> {
  const body = await apiRequest("GET", id ? `/vault/signing/${encodeURIComponent(id)}` : "/vault/signing");
  return Array.isArray(body) ? (body as SigningInfo[]) : [body as SigningInfo];
}
