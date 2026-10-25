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

const CREATE_TOKEN_HELP = process.env.VAULT_TOKEN_COMMAND
  ? "ask the user to run `pvault create-service-token <name> --scope \"identity.*,...\" --keychain <service>` " +
    "in their terminal (the service VAULT_TOKEN_COMMAND reads); it is picked up on the next call, no restart needed"
  : "ask the user to create one with `pvault create-service-token <name> --scope \"identity.*,...\"` " +
    "and set it as VAULT_TOKEN in this MCP server's env";

const MISSING_TOKEN_HELP = `vault: no service token — ${CREATE_TOKEN_HELP}`;

/** Token from VAULT_TOKEN_COMMAND, kept until the server rejects it. */
let commandToken: string | undefined;

/**
 * The MCP server only ever uses a scoped service token supplied by the user,
 * either directly in VAULT_TOKEN or printed by VAULT_TOKEN_COMMAND (e.g. a
 * Keychain lookup). It never reads ~/.pvault/.session: that token has full,
 * uncapped access and would bypass the scope and sensitivity limits.
 */
async function resolveToken(): Promise<string> {
  const token = process.env.VAULT_TOKEN?.trim();
  if (token) {
    return token;
  }
  const command = process.env.VAULT_TOKEN_COMMAND?.trim();
  if (!command) {
    throw new VaultError(MISSING_TOKEN_HELP);
  }
  if (!commandToken) {
    try {
      const { stdout } = await execFileAsync("/bin/sh", ["-c", command], { timeout: 10_000 });
      commandToken = stdout.trim() || undefined;
    } catch {
      // No stored token yet (or the lookup failed): fall through to the help text.
    }
  }
  if (!commandToken) {
    throw new VaultError(MISSING_TOKEN_HELP);
  }
  return commandToken;
}

/**
 * Fails fast when no token source is configured at all. With only
 * VAULT_TOKEN_COMMAND the server starts even before a token is stored, so
 * the user can create one mid-session.
 */
export function assertTokenConfigured(): void {
  if (!process.env.VAULT_TOKEN?.trim() && !process.env.VAULT_TOKEN_COMMAND?.trim()) {
    throw new VaultError(MISSING_TOKEN_HELP);
  }
}

/**
 * Sends an authenticated request. A token from VAULT_TOKEN_COMMAND that the
 * server rejects is re-read once, so a token replaced mid-session just works.
 */
async function authedFetch(path: string, init: RequestInit): Promise<Response> {
  for (let attempt = 0; ; attempt++) {
    const token = await resolveToken();
    const headers = new Headers(init.headers);
    headers.set("Authorization", `Bearer ${token}`);
    let resp: Response;
    try {
      resp = await fetch(`${serverAddr()}${path}`, { ...init, headers });
    } catch {
      throw new VaultError("vault: server not running — run 'pvault unlock'");
    }
    if (resp.status === 401 && token === commandToken) {
      commandToken = undefined;
      if (attempt === 0) continue;
    }
    if (!resp.ok) {
      throw mapError(resp.status, await resp.text());
    }
    return resp;
  }
}

function mapError(status: number, body: string): VaultError {
  if (status === 401) {
    return new VaultError(
      `vault: service token invalid, expired or revoked — ${CREATE_TOKEN_HELP}`
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
