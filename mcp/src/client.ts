import { exec } from "node:child_process";
import { userInfo } from "node:os";
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

function serverAddr(): string {
  return process.env.VAULT_ADDR ?? "http://127.0.0.1:7200";
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
 * How to get on without a working token: Touch ID first, since with it on the user
 * approves `pvault get`/`pvault set` from the shell and no token is needed; else a
 * new token, matching whichever source is actually used.
 */
function createTokenHelp(): string {
  const touchID = "if `pvault touchid status` says on, use `pvault get <id> ...` or `pvault set` " +
    "from the shell instead (the user approves with Touch ID, no token needed); otherwise ";
  const create = "`pvault create-service-token <name> --scope \"identity.*,...\"";
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

async function authedFetch(path: string, init: RequestInit): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${await resolveToken()}`);
  let resp: Response;
  try {
    resp = await fetch(`${serverAddr()}${path}`, { ...init, headers });
  } catch {
    throw new VaultError("vault: server not running — run 'pvault unlock'");
  }
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
      return new VaultError("vault: vault is locked — ask the user to run 'pvault unlock'");
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
        "vault: personal items (SPID/CIE credentials) can only be changed by the user — send them to the vault UI"
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
  const url = `${serverAddr()}/vault/status`;
  let resp: Response;
  try {
    resp = await fetch(url);
  } catch {
    throw new VaultError(
      "vault: server not running — run 'pvault unlock'"
    );
  }
  if (!resp.ok) {
    throw new VaultError(`vault: status check failed — HTTP ${resp.status}`);
  }
  return resp.json() as Promise<VaultStatus>;
}

export async function getSchema(): Promise<unknown> {
  // Public endpoint — works without auth
  const url = `${serverAddr()}/vault/schema`;
  let resp: Response;
  try {
    resp = await fetch(url);
  } catch {
    throw new VaultError(
      "vault: server not running — run 'pvault unlock'"
    );
  }
  if (!resp.ok) {
    throw new VaultError(`vault: schema fetch failed — HTTP ${resp.status}`);
  }
  return resp.json();
}

// Field IDs go into the URL path, so only the vault's own ID shape is sent
// ([member:]category.field_name: letters, digits, _ and -, and a member slug
// of lower-case letters, digits and -). A "/" or ".." could otherwise reach
// another endpoint past VAULT_SCOPE.
const FIELD_ID = /^(?:(?!io:)[a-z][a-z0-9-]{0,31}:)?[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/;

function fieldPath(id: string): string {
  if (!FIELD_ID.test(id)) {
    throw new VaultError(
      `vault: invalid field ID ${JSON.stringify(id)} — use category.field_name, or member:category.field_name for a family member (letters, digits, _ and -)`
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
  member?: string; // a family member's slug; absent for the owner
  expires_on: string;
  days_left: number;
  expired: boolean;
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
  famiglia?: Record<string, string>;
  first_name?: string;
  last_name?: string;
  age?: number;
  minor?: boolean;
  anpr_relazione?: string;
  relazione_label?: string;
  fields?: number;
  documents?: number;
}

export interface Household {
  intestatario_scheda?: string;
  members: MemberInfo[];
}

export async function listMembers(): Promise<Household> {
  return apiRequest("GET", "/vault/members") as Promise<Household>;
}

export async function getExpiring(days: number): Promise<ExpiryInfo[]> {
  return apiRequest("GET", `/vault/expiring?days=${days}`) as Promise<ExpiryInfo[]>;
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
    body: new Blob([new Uint8Array(data)]),
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

export type { VaultStatus, FieldInfo, ContextBundle, ExpiryInfo, DocumentInfo };
