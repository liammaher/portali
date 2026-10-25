import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir, homedir } from "node:os";
import { join } from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

// The real MCP server over stdio, against a fake vault whose token may read
// everything, so only the server's own VAULT_SCOPE and import rules apply.
let vault: Server;
let importDir: string;
let client: Client;

before(async () => {
  vault = createServer((req, res) => {
    res.setHeader("Content-Type", "application/json");
    if (req.url?.startsWith("/vault/expiring")) {
      res.end(JSON.stringify([
        { field_id: "passaporto.data_scadenza", document: "passaporto", expires_on: "2026-11-01", days_left: 7 },
        { field_id: "residenza.data_scadenza", document: "residenza", expires_on: "2026-11-02", days_left: 8 },
      ]));
      return;
    }
    res.end("{}");
  });
  await new Promise<void>((r) => vault.listen(0, "127.0.0.1", r));
  importDir = await mkdtemp(join(tmpdir(), "vault-mcp-import-"));
  client = new Client({ name: "test", version: "1" });
  await client.connect(new StdioClientTransport({
    command: process.execPath,
    args: ["--import", "tsx", "src/index.ts"],
    env: {
      ...(process.env as Record<string, string>),
      VAULT_ADDR: `http://127.0.0.1:${(vault.address() as AddressInfo).port}`,
      VAULT_TOKEN: "pvs_" + "a".repeat(64),
      VAULT_SCOPE: "residenza.*",
      VAULT_IMPORT_DIRS: importDir,
      PEC_ADDRESS: "",
    },
  }));
});

after(async () => {
  await client?.close();
  vault?.close();
  await rm(importDir, { recursive: true, force: true });
});

const text = (r: unknown) => ((r as { content: { text: string }[] }).content[0]?.text ?? "");

test("vault_expiring keeps to VAULT_SCOPE", async () => {
  const out = text(await client.callTool({ name: "vault_expiring", arguments: {} }));
  assert.match(out, /residenza\.data_scadenza/);
  assert.doesNotMatch(out, /passaporto/);
});

test("vault_document_add doesn't tell which paths exist outside the import folders", async () => {
  const answers = [];
  for (const path of ["/etc/hosts", "/etc/nonexistent-xyz", join(homedir(), ".ssh", "nope-xyz")]) {
    answers.push(text(await client.callTool({ name: "vault_document_add", arguments: { path, category: "residenza" } })));
  }
  for (const a of answers) assert.match(a, /only files inside/);
  // Inside an import folder the real reason is still given.
  const missing = text(await client.callTool({
    name: "vault_document_add", arguments: { path: join(importDir, "missing.pdf"), category: "residenza" },
  }));
  assert.match(missing, /ENOENT|no such file/);
});
