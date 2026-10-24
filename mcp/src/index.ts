#!/usr/bin/env node
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { readFile, realpath, stat } from "node:fs/promises";
import { homedir } from "node:os";
import { basename, extname, join, relative, isAbsolute, sep } from "node:path";
import {
  getStatus, getSchema, getField, listFields, getContext, setField, assertTokenConfigured,
  getExpiring, listDocuments, addDocument, getDocument,
} from "./client.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { getScope, scopeAllows, scopeAllowsCategory } from "./scope.js";

function ok(text: string): CallToolResult {
  return { content: [{ type: "text", text }] };
}

function err(message: string): CallToolResult {
  return { content: [{ type: "text", text: message }], isError: true };
}

const scope = getScope();

const server = new McpServer({
  name: "vault",
  version: "0.1.0",
});

server.registerTool(
  "vault_status",
  {
    description:
      "Check if the vault server is running and unlocked. Returns initialization state, lock status, field count, and categories. This is a public endpoint that works without authentication.",
  },
  async (): Promise<CallToolResult> => {
    try {
      const status = await getStatus();
      return ok(JSON.stringify(status, null, 2));
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

server.registerTool(
  "vault_schema",
  {
    description:
      "Retrieve the recommended vault schema with canonical field names, descriptions, and default sensitivity tiers. Use this to discover the correct field IDs before writing to the vault. This is a public endpoint that works without authentication.",
  },
  async (): Promise<CallToolResult> => {
    try {
      const schema = await getSchema();
      return ok(JSON.stringify(schema, null, 2));
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

server.registerTool(
  "vault_get",
  {
    description:
      "Retrieve a single decrypted field from the vault. Returns the field value, category, sensitivity tier, and metadata.",
    inputSchema: {
      id: z
        .string()
        .describe(
          "Field ID in category.field_name format, e.g. identity.full_name"
        ),
    },
  },
  async ({ id }): Promise<CallToolResult> => {
    if (!scopeAllows(scope, id)) {
      return err(`scope denied: ${id} is not in VAULT_SCOPE`);
    }
    try {
      const field = await getField(id);
      return ok(JSON.stringify(field, null, 2));
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

server.registerTool(
  "vault_list",
  {
    description:
      "List all field metadata in the vault (IDs, categories, sensitivity tiers). Does not return decrypted values — use vault_get or vault_context for values.",
  },
  async (): Promise<CallToolResult> => {
    try {
      const fields = await listFields();
      const filtered = fields.filter((f) => scopeAllows(scope, f.id));
      return ok(JSON.stringify(filtered, null, 2));
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

server.registerTool(
  "vault_context",
  {
    description:
      "Retrieve all decrypted fields this token may read, grouped by category. Fields outside the token's scope or above its sensitivity cap are omitted. All returned data passes through the LLM provider's inference pipeline.",
  },
  async (): Promise<CallToolResult> => {
    try {
      const ctx = await getContext();
      for (const cat of Object.keys(ctx.categories)) {
        if (!scopeAllowsCategory(scope, cat)) {
          delete ctx.categories[cat];
          continue;
        }
        ctx.categories[cat] = ctx.categories[cat].filter((f) =>
          scopeAllows(scope, f.id)
        );
        if (ctx.categories[cat].length === 0) {
          delete ctx.categories[cat];
        }
      }
      return ok(JSON.stringify(ctx, null, 2));
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

server.registerTool(
  "vault_set",
  {
    description:
      "Save a field to the user's encrypted vault. Creates or updates a field. Use this when the user provides personal information that isn't already in the vault (e.g. t-shirt size, dietary preferences). Always ask the user for confirmation before saving: 'Want me to save M as your t-shirt size for next time?' Call vault_schema first to discover recommended field names.",
    inputSchema: {
      id: z
        .string()
        .describe(
          "Field ID in category.field_name format, e.g. identity.tshirt_size"
        ),
      value: z.string().describe("The value to encrypt and store"),
      sensitivity: z
        .enum(["public", "standard", "sensitive", "critical"])
        .optional()
        .describe("Sensitivity tier (default: standard)"),
    },
  },
  async ({ id, value, sensitivity }): Promise<CallToolResult> => {
    if (!scopeAllows(scope, id)) {
      return err(`scope denied: ${id} is not in VAULT_SCOPE`);
    }
    try {
      await setField(id, value, sensitivity);
      return ok(`Saved ${id}`);
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

server.registerTool(
  "vault_expiring",
  {
    description:
      "List the user's documents (permesso di soggiorno, passaporto, CIE, patente, tessera sanitaria, ...) that have expired or expire within the given number of days, soonest first. Use this to remind the user to renew documents in time.",
    inputSchema: {
      days: z.number().int().min(0).max(3650).optional().describe("Look-ahead window in days (default 90)"),
    },
  },
  async ({ days }): Promise<CallToolResult> => {
    try {
      return ok(JSON.stringify(await getExpiring(days ?? 90), null, 2));
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

server.registerTool(
  "vault_documents",
  {
    description:
      "List encrypted document scans stored in the vault (id, category, filename, type, size). Use vault_document_get to read one.",
    inputSchema: {
      category: z.string().optional().describe("Only this category, e.g. permesso_soggiorno"),
    },
  },
  async ({ category }): Promise<CallToolResult> => {
    try {
      const docs = await listDocuments(category);
      return ok(JSON.stringify(docs.filter((d) => scopeAllows(scope, `${d.category}.documents`)), null, 2));
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

const MAX_DOCUMENT_BYTES = 20 * 1024 * 1024;
const MAX_INLINE_BYTES = 10 * 1024 * 1024;
const MIME_BY_EXT: Record<string, string> = {
  ".pdf": "application/pdf",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".png": "image/png",
  ".heic": "image/heic",
  ".webp": "image/webp",
  ".tif": "image/tiff",
  ".tiff": "image/tiff",
  ".p7m": "application/pkcs7-mime",
  ".xml": "application/xml",
  ".txt": "text/plain",
};

// Folders vault_document_add may read from. Without this limit an agent that
// only has this MCP server could archive any local file (SSH keys, the vault's
// own secret.key) and read it back with vault_document_get.
const IMPORT_DIRS = (process.env.VAULT_IMPORT_DIRS ?? join(homedir(), "Downloads"))
  .split(":")
  .filter((d) => d.trim() !== "");

/** Resolve path and confirm it is a non-hidden file inside an import folder. */
async function resolveImportPath(path: string): Promise<string> {
  if (!isAbsolute(path)) throw new Error("path must be absolute");
  const real = await realpath(path);
  for (const dir of IMPORT_DIRS) {
    let root: string;
    try {
      root = await realpath(dir);
    } catch {
      continue;
    }
    const rel = relative(root, real);
    if (rel === "" || rel.startsWith("..") || isAbsolute(rel)) continue;
    if (rel.split(sep).some((part) => part.startsWith("."))) {
      throw new Error("hidden files and folders can't be imported");
    }
    return real;
  }
  throw new Error(
    `only files inside ${IMPORT_DIRS.join(", ")} can be imported (set VAULT_IMPORT_DIRS to change this)`
  );
}

server.registerTool(
  "vault_document_add",
  {
    description:
      "Encrypt and archive a local file (e.g. a receipt, protocol confirmation or certificate you just downloaded from a government portal) in the user's vault. Only files inside the import folders (default ~/Downloads, set by VAULT_IMPORT_DIRS) can be archived. Ask the user before archiving, and suggest deleting the unencrypted original afterwards.",
    inputSchema: {
      path: z.string().describe("Absolute path of the local file to store"),
      category: z.string().describe("Category, e.g. permesso_soggiorno, passaporto, residenza, business"),
      sensitivity: z.enum(["public", "standard", "sensitive", "critical"]).optional().describe("Sensitivity tier (default: sensitive)"),
    },
  },
  async ({ path, category, sensitivity }): Promise<CallToolResult> => {
    if (!scopeAllows(scope, `${category}.documents`)) {
      return err(`scope denied: ${category}.documents is not in VAULT_SCOPE`);
    }
    try {
      const real = await resolveImportPath(path);
      const info = await stat(real);
      if (!info.isFile()) return err(`${path} is not a file`);
      if (info.size > MAX_DOCUMENT_BYTES) return err(`${path} is larger than 20 MB`);
      const data = await readFile(real);
      const type = MIME_BY_EXT[extname(path).toLowerCase()] ?? "application/octet-stream";
      const doc = await addDocument(category, basename(path), type, data, sensitivity);
      return ok(`Stored ${doc.name} as document ${doc.id} in ${doc.category} [${doc.sensitivity}]`);
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

server.registerTool(
  "vault_document_get",
  {
    description:
      "Read an encrypted document from the vault by id. Images are returned as images; other files (e.g. PDFs) as an embedded resource. The content passes through the LLM provider's inference pipeline.",
    inputSchema: {
      id: z.string().describe("Document id from vault_documents"),
    },
  },
  async ({ id }): Promise<CallToolResult> => {
    try {
      const doc = await getDocument(id);
      if (!scopeAllows(scope, `${doc.category}.documents`)) {
        return err(`scope denied: ${doc.category}.documents is not in VAULT_SCOPE`);
      }
      if (doc.data.length > MAX_INLINE_BYTES) {
        return err("document is larger than 10 MB and can't be returned inline");
      }
      const blob = doc.data.toString("base64");
      if (doc.contentType.startsWith("image/")) {
        return { content: [{ type: "image", data: blob, mimeType: doc.contentType }] };
      }
      return {
        content: [{ type: "resource", resource: { uri: `vault://documents/${id}`, mimeType: doc.contentType, blob } }],
      };
    } catch (e) {
      return err((e as Error).message);
    }
  }
);

async function main() {
  // Fail fast: the server never falls back to the full-access session token.
  assertTokenConfigured();

  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((e) => {
  process.stderr.write(`vault-mcp fatal: ${e}\n`);
  process.exit(1);
});
