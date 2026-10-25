import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

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

const TOKEN_COMMAND_TIMEOUT_MS = 10_000;

/** Printable ASCII without spaces: anything else can't go in a Bearer header. */
const TOKEN_PATTERN = /^[\x21-\x7e]+$/;

function staticToken(): string | undefined {
  return process.env.VAULT_TOKEN?.trim() || undefined;
}

function tokenCommand(): string | undefined {
  return process.env.VAULT_TOKEN_COMMAND?.trim() || undefined;
}

/** How to get a working token, matching whichever source is actually used. */
function createTokenHelp(): string {
  if (!staticToken() && tokenCommand()) {
    return (
      "ask the user to run `pvault create-service-token <name> --scope \"identity.*,...\" --keychain <service>` " +
      "in their terminal (the service VAULT_TOKEN_COMMAND reads); the next call uses it, no restart needed"
    );
  }
  return (
    "ask the user to create one with `pvault create-service-token <name> --scope \"identity.*,...\"` " +
    "and set it as VAULT_TOKEN in this MCP server's env"
  );
}

function missingToken(detail?: string): VaultError {
  const why = detail ? ` (${detail})` : "";
  return new VaultError(`vault: no service token${why} — ${createTokenHelp()}`);
}

/** First line of a command's stderr, or a description of how it failed. */
function commandFailure(e: unknown): string {
  const err = e as { killed?: boolean; signal?: string; code?: number | string; stderr?: string };
  if (err.killed || err.signal) {
    return `VAULT_TOKEN_COMMAND timed out after ${TOKEN_COMMAND_TIMEOUT_MS / 1000}s; a Keychain prompt may be waiting`;
  }
  const line = err.stderr?.trim().split("\n")[0];
  return `VAULT_TOKEN_COMMAND failed: ${line || `exit ${err.code ?? "unknown"}`}`;
}

/**
 * The MCP server only ever uses a scoped service token supplied by the user,
 * either directly in VAULT_TOKEN or printed by VAULT_TOKEN_COMMAND (e.g. a
 * Keychain lookup). It never reads ~/.pvault/.session: that token has full,
 * uncapped access and would bypass the scope and sensitivity limits.
 *
 * The command runs for every request rather than being cached, so a token
 * the user replaces mid-session is used right away, even while the old one
 * is still valid.
 */
async function resolveToken(): Promise<string> {
  let token = staticToken();
  let source = "VAULT_TOKEN";
  if (!token) {
    const command = tokenCommand();
    if (!command) {
      throw missingToken();
    }
    source = "VAULT_TOKEN_COMMAND output";
    try {
      const { stdout } = await execFileAsync("/bin/sh", ["-c", command], {
        timeout: TOKEN_COMMAND_TIMEOUT_MS,
      });
      token = stdout.trim();
    } catch (e) {
      throw missingToken(commandFailure(e));
    }
    if (!token) {
      throw missingToken("VAULT_TOKEN_COMMAND printed nothing");
    }
  }
  if (!TOKEN_PATTERN.test(token)) {
    throw new VaultError(`vault: ${source} is not a valid token (it contains spaces, line breaks or non-ASCII characters)`);
  }
  return token;
}

/**
 * Fails fast when no token source is configured at all. With only
 * VAULT_TOKEN_COMMAND the server starts even before a token is stored, so
 * the user can create one mid-session.
 */
export function assertTokenConfigured(): void {
  if (!staticToken() && !tokenCommand()) {
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

export async function getField(id: string): Promise<FieldInfo> {
  return apiRequest("GET", `/vault/fields/${id}`) as Promise<FieldInfo>;
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
  return apiRequest("PUT", `/vault/fields/${id}`, body) as Promise<{
    status: string;
  }>;
}

interface ExpiryInfo {
  field_id: string;
  document: string;
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
