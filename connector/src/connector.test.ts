import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { serve } from "./http.js";
import { loadRegistry } from "./registry.js";

// The real server over HTTP, against a registry laid out as plugin/build.py writes it.
// REGISTRY_DIR (a built plugin's portali-pratica folder) adds a check on the real one.
let dir: string;
let http: Server;
let url: string;
let client: Client;
const steps = JSON.stringify({ procedure: "napoli/comune/cila-lavori-edilizi", asks: { titolo: { question: "The client's title" } }, yours: ["Sign and send"] });
const sha = createHash("sha256").update(steps).digest("hex");

before(async () => {
  dir = await mkdtemp(join(tmpdir(), "napoli-connector-"));
  await mkdir(join(dir, "pratiche"));
  await mkdir(join(dir, "library", "procedures", "napoli", "comune"), { recursive: true });
  await writeFile(join(dir, "pratiche", "cila-fittizia.json"), steps);
  await writeFile(join(dir, "catalog.json"), JSON.stringify([
    { name: "cila-fittizia", procedure: "napoli/comune/cila-lavori-edilizi", title: "CILA: lavori interni non strutturali", refuses: ["So far only a tenant"], notes: "c the client", steps_sha256: sha },
  ]));
  await writeFile(join(dir, "coverage.json"), JSON.stringify({
    plugin_version: "0.1.0", built: "2026-10-29", comuni: { F839: "Napoli", IT: "nazionale" },
    pratiche: [
      { name: "cila-fittizia", procedure: "napoli/comune/cila-lavori-edilizi", title: "CILA: lavori interni non strutturali", comune: "F839", sportello: "sue", status: "available", avatars: ["architetto", "geometra"], last_run: "2026-10-28", changed: [], steps_sha256: sha, library: true },
      { name: null, procedure: "italia/agenzia-entrate/apertura-partita-iva", title: "Apertura della partita IVA", comune: "IT", sportello: null, status: "guide", avatars: [], library: true },
      { name: null, title: "SCIA per un bar", comune: "F839", sportello: "suap", status: "coming", avatars: ["impresa"], library: false, expected: "2026-12" },
    ],
  }));
  await writeFile(join(dir, "library", "procedures", "napoli", "comune", "cila-lavori-edilizi.yaml"),
    "title: CILA a Napoli\naka:\n  - ristrutturazione interna\n  - spostare tramezzi\nsummary: lavori interni che non toccano le strutture\n");
  http = await serve(await loadRegistry(dir), 0);
  url = `http://127.0.0.1:${(http.address() as AddressInfo).port}/mcp`;
  client = new Client({ name: "test", version: "0" });
  await client.connect(new StreamableHTTPClientTransport(new URL(url)));
});

after(async () => {
  await client.close();
  http.close();
  await rm(dir, { recursive: true, force: true });
});

function json(r: Awaited<ReturnType<Client["callTool"]>>): any {
  return JSON.parse((r.content as Array<{ text: string }>)[0].text);
}

test("every tool is read-only and titled, and the instructions say what never goes to the server", async () => {
  const { tools } = await client.listTools();
  assert.deepEqual(tools.map((t) => t.name).sort(), ["coverage", "find_pratica", "get_pratica", "get_procedure"]);
  for (const t of tools) {
    assert.equal(t.annotations?.readOnlyHint, true, t.name);
    assert.equal(t.annotations?.destructiveHint, false, t.name);
    assert.ok(t.title, t.name);
  }
  assert.match(client.getInstructions() ?? "", /no personal data/);
});

test("coverage counts by status, and an architect in Napoli also sees national procedures", async () => {
  const all = json(await client.callTool({ name: "coverage", arguments: {} }));
  assert.deepEqual(all.counts, { available: 1, paused: 0, mapped: 0, guide: 1, coming: 1 });
  const arch = json(await client.callTool({ name: "coverage", arguments: { comune: "napoli", avatar: "Architetto" } }));
  assert.deepEqual(arch.pratiche.map((p: { status: string }) => p.status), ["available", "guide"]);
  assert.equal(arch.pratiche[0].last_run, "2026-10-28");
});

test("a case described by its facts finds the pratica through the library's other names for it", async () => {
  const r = json(await client.callTool({ name: "find_pratica", arguments: { query: "devo spostare dei tramezzi in casa" } }));
  assert.equal(r.found[0].name, "cila-fittizia");
  assert.deepEqual(r.found[0].matched, ["spostare", "tramezzi"]);
  const none = json(await client.callTool({ name: "find_pratica", arguments: { query: "rinnovo passaporto" } }));
  assert.deepEqual(none.found, []);
  assert.deepEqual(none.unknown_words, ["rinnovo", "passaporto"]);
  const half = json(await client.callTool({ name: "find_pratica", arguments: { query: "tramezzi e passaporto" } }));
  assert.equal(half.found[0].name, "cila-fittizia");
  assert.deepEqual(half.unknown_words, ["passaporto"]);
});

test("get_pratica says whether the plugin's copy is current, and sends the steps only when asked", async () => {
  const current = await client.callTool({ name: "get_pratica", arguments: { name: "cila-fittizia", local_sha256: sha } });
  assert.equal(json(current).copy, "current");
  assert.equal((current.content as unknown[]).length, 1);
  assert.deepEqual(json(current).asks, [{ key: "titolo", question: "The client's title" }]);
  const stale = await client.callTool({ name: "get_pratica", arguments: { name: "cila-fittizia", local_sha256: "0".repeat(64), include_steps: true } });
  assert.equal(json(stale).copy, "stale");
  assert.equal((stale.content as Array<{ text: string }>)[1].text, steps);
  const missing = await client.callTool({ name: "get_pratica", arguments: { name: "nessuna" } });
  assert.equal(missing.isError, true);
});

test("get_procedure serves the library entry, and only ids shaped like one", async () => {
  const r = await client.callTool({ name: "get_procedure", arguments: { procedure: "napoli/comune/cila-lavori-edilizi" } });
  assert.match((r.content as Array<{ text: string }>)[0].text, /^title: CILA a Napoli/);
  const gap = await client.callTool({ name: "get_procedure", arguments: { procedure: "napoli/comune/agibilita" } });
  assert.equal(gap.isError, true);
  const bad = await client.callTool({ name: "get_procedure", arguments: { procedure: "../../etc/passwd" } });
  assert.equal(bad.isError, true);
});

test("a page on another origin is refused; no Origin (Claude's cloud) or Claude's own passes", async () => {
  const init = { jsonrpc: "2.0", id: 1, method: "tools/list", params: {} };
  const post = (origin?: string) => fetch(url, {
    method: "POST", body: JSON.stringify(init),
    headers: { "Content-Type": "application/json", Accept: "application/json, text/event-stream", ...(origin ? { Origin: origin } : {}) },
  });
  assert.equal((await post("https://evil.example")).status, 403);
  assert.equal((await post("https://claude.ai")).status, 200);
  assert.equal((await fetch(url)).status, 405);
});

test("the real registry loads, and every pratica it lists can be served", { skip: !process.env.REGISTRY_DIR }, async () => {
  const reg = await loadRegistry(process.env.REGISTRY_DIR!);
  const named = reg.entries.filter((e) => e.name);
  assert.ok(named.length > 0);
  for (const e of named) {
    const c = reg.catalog.get(e.name!);
    assert.ok(c, e.name!);
    assert.equal(createHash("sha256").update(reg.steps.get(e.name!)!).digest("hex"), c.steps_sha256, e.name!);
  }
});
