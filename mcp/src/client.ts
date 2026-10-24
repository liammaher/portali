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

const MISSING_TOKEN_HELP =
  "vault: no service token — create one with " +
  "`pvault create-service-token <name> --scope \"identity.*,...\"` " +
  "and set it as VAULT_TOKEN in this MCP server's env";

/**
 * The MCP server only ever uses a scoped service token supplied by the user
 * via VAULT_TOKEN. It never reads ~/.pvault/.session: that token has full,
 * uncapped access and would bypass the scope and sensitivity limits.
 */
function resolveToken(): string {
  const token = process.env.VAULT_TOKEN?.trim();
  if (!token) {
    throw new VaultError(MISSING_TOKEN_HELP);
  }
  return token;
}

export function assertTokenConfigured(): void {
  resolveToken();
}

function mapError(status: number, body: string): VaultError {
  if (status === 401) {
    return new VaultError(
      "vault: service token invalid, expired or revoked — create a new one with `pvault create-service-token`"
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
  const token = resolveToken();
  const url = `${serverAddr()}${path}`;

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${token}`,
  };

  let resp: Response;
  try {
    resp = await fetch(url, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch (err) {
    throw new VaultError(
      "vault: server not running — run 'pvault unlock'"
    );
  }

  if (!resp.ok) {
    const text = await resp.text();
    throw mapError(resp.status, text);
  }

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
  const resp = await rawFetch(`/vault/documents?${q}`, {
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
  const resp = await rawFetch(`/vault/documents/${encodeURIComponent(id)}`, { method: "GET" });
  return {
    contentType: resp.headers.get("Content-Type") ?? "application/octet-stream",
    category: resp.headers.get("X-Document-Category") ?? "",
    data: Buffer.from(await resp.arrayBuffer()),
  };
}

async function rawFetch(path: string, init: RequestInit): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${resolveToken()}`);
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

export type { VaultStatus, FieldInfo, ContextBundle, ExpiryInfo, DocumentInfo };
