import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir, homedir } from "node:os";
import { join } from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { fakeVaultSocket } from "./vault-socket.testutil.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

// The real MCP server over stdio, against a fake vault whose token may read
// everything, so only the server's own import rules apply.
let vault: Server;
let vaultDir: string;
let importDir: string;
let client: Client;
let lastSigning: { name: string; purpose: string; pdf: string; placements: unknown[] } | undefined;

before(async () => {
  ({ dir: vaultDir, server: vault } = await fakeVaultSocket((req, res) => {
    res.setHeader("Content-Type", "application/json");
    if (req.url?.startsWith("/vault/expiring")) {
      res.end(JSON.stringify([
        { field_id: "passaporto.data_scadenza", document: "passaporto", expires_on: "2026-11-01", days_left: 7 },
        { field_id: "residenza.data_scadenza", document: "residenza", expires_on: "2026-11-02", days_left: 8 },
        { field_id: "leo:residenza.data_scadenza", document: "residenza", member: "leo", expires_on: "2026-11-03", days_left: 9 },
      ]));
      return;
    }
    if (req.url === "/vault/members") {
      res.end(JSON.stringify({
        intestatario_scheda: "io",
        members: [
          { slug: "leo", famiglia: { relazione: "figlio" }, first_name: "Leo", age: 6, minor: true, anpr_relazione: "3", relazione_label: "figlio", fields: 5 },
          { slug: "giulia", famiglia: { relazione: "coniuge" }, first_name: "Giulia", fields: 2 },
        ],
      }));
      return;
    }
    if (req.method === "POST" && req.url === "/vault/signing") {
      const chunks: Buffer[] = [];
      req.on("data", (c) => chunks.push(c));
      req.on("end", () => {
        lastSigning = JSON.parse(Buffer.concat(chunks).toString());
        res.end(JSON.stringify({
          id: "a".repeat(32), name: lastSigning!.name, requester: "agent", signer: "io", status: "pending",
          pages: 1, date: "2026-10-27", expires_at: "2026-10-27T11:00:00Z",
        }));
      });
      return;
    }
    if (req.url === "/vault/signing") {
      res.end(JSON.stringify([{ id: "a".repeat(32), name: "mod-1.pdf", requester: "agent", signer: "io", status: "pending", pages: 1, date: "2026-10-27", expires_at: "2026-10-27T11:00:00Z", sha256: "x" }]));
      return;
    }
    res.end("{}");
  }));
  importDir = await mkdtemp(join(tmpdir(), "vault-mcp-import-"));
  client = new Client({ name: "test", version: "1" });
  await client.connect(new StdioClientTransport({
    command: process.execPath,
    args: ["--import", "tsx", "src/index.ts"],
    env: {
      ...(process.env as Record<string, string>),
      VAULT_DIR: vaultDir,
      VAULT_ADDR: "",
      VAULT_TOKEN: "pvs_" + "a".repeat(64),
      VAULT_IMPORT_DIRS: importDir,
      PEC_ADDRESS: "",
    },
  }));
});

after(async () => {
  await client?.close();
  vault?.close();
  await rm(importDir, { recursive: true, force: true });
  await rm(vaultDir, { recursive: true, force: true });
});

const text = (r: unknown) => ((r as { content: { text: string }[] }).content[0]?.text ?? "");

test("vault_household passes on what the vault's scope allows", async () => {
  const out = JSON.parse(text(await client.callTool({ name: "vault_household", arguments: {} })));
  assert.deepEqual(out.members.map((m: { slug: string }) => m.slug), ["leo", "giulia"]);
});

test("refuses to start with VAULT_SCOPE set", async () => {
  const { spawnSync } = await import("node:child_process");
  const run = spawnSync(process.execPath, ["--import", "tsx", "src/index.ts"], {
    env: { ...process.env, VAULT_SCOPE: "residenza.*", VAULT_TOKEN: "pvs_" + "a".repeat(64) },
    input: "", encoding: "utf8",
  });
  assert.notEqual(run.status, 0);
  assert.match(run.stderr, /VAULT_SCOPE is no longer supported/);
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

const PLACEMENT = { page: 1, rect: [401.1, 729, 533.4, 759], date_rect: [68, 748.5, 172.2, 759] };

test("signing: there is a way to ask and to watch, and none to approve", async () => {
  const names = (await client.listTools()).tools.map((t) => t.name);
  assert.ok(names.includes("vault_sign_request"));
  assert.ok(names.includes("vault_sign_status"));
  const approvers = names.filter((n) => /approve|review|confirm|sign_grant|signed_file|candidate/i.test(n));
  assert.deepEqual(approvers, [], "a tool could approve or read a signature: the approval is the user's alone");
});

test("vault_sign_request sends the PDF and placements, and says what the user does", async () => {
  const pdf = join(importDir, "mod-1.pdf");
  await writeFile(pdf, "%PDF-1.4\nbody");
  const out = text(await client.callTool({
    name: "vault_sign_request",
    arguments: { path: pdf, purpose: "TARI Mod. 1", placements: [PLACEMENT] },
  }));
  assert.equal(lastSigning?.name, "mod-1.pdf");
  assert.equal(lastSigning?.purpose, "TARI Mod. 1");
  assert.equal(Buffer.from(lastSigning!.pdf, "base64").toString(), "%PDF-1.4\nbody");
  assert.deepEqual(lastSigning?.placements, [PLACEMENT]);
  assert.match(out, /pvault sign review a{32}/);
  assert.match(out, /Nothing is signed yet/);
});

test("vault_sign_request refuses files outside the import folders and non-PDFs", async () => {
  const outside = await client.callTool({
    name: "vault_sign_request",
    arguments: { path: "/etc/hosts", purpose: "x", placements: [PLACEMENT] },
  });
  assert.equal((outside as { isError?: boolean }).isError, true);
  const notPdf = join(importDir, "note.pdf");
  await writeFile(notPdf, "hello");
  const bad = await client.callTool({
    name: "vault_sign_request",
    arguments: { path: notPdf, purpose: "x", placements: [PLACEMENT] },
  });
  assert.equal((bad as { isError?: boolean }).isError, true);
  assert.match(text(bad), /not a PDF/);
});

test("vault_sign_status lists the requests without the file or its hash", async () => {
  const out = text(await client.callTool({ name: "vault_sign_status", arguments: {} }));
  const list = JSON.parse(out);
  assert.equal(list[0].status, "pending");
  assert.ok(!("sha256" in list[0]));
});
