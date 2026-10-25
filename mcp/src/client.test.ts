import { test, before, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { assertTokenConfigured, getField } from "./client.js";

// Fake vault: each valid token may read fields under one category prefix.
const tokens = new Map<string, string>();
const seen: string[] = [];
let server: Server;
let dir: string;
let tokenFile: string;

before(async () => {
  server = createServer((req, res) => {
    const token = (req.headers.authorization ?? "").replace(/^Bearer /, "");
    seen.push(token);
    const id = decodeURIComponent((req.url ?? "").replace("/vault/fields/", ""));
    const prefix = tokens.get(token);
    res.setHeader("Content-Type", "application/json");
    if (prefix === undefined) {
      res.writeHead(401).end(JSON.stringify({ error: "unauthorized" }));
    } else if (!id.startsWith(prefix)) {
      res.writeHead(403).end(JSON.stringify({ constraint: "scope_exceeded" }));
    } else {
      res.end(JSON.stringify({ id, value: `value-of-${id}` }));
    }
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  process.env.VAULT_ADDR = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  dir = await mkdtemp(join(tmpdir(), "vault-mcp-test-"));
  tokenFile = join(dir, "token");
});

after(async () => {
  server.close();
  await rm(dir, { recursive: true, force: true });
});

beforeEach(async () => {
  tokens.clear();
  seen.length = 0;
  delete process.env.VAULT_TOKEN;
  process.env.VAULT_TOKEN_COMMAND = `cat '${tokenFile}'`;
  await rm(tokenFile, { force: true });
});

test("refuses to start with no token source at all", () => {
  delete process.env.VAULT_TOKEN_COMMAND;
  assert.throws(assertTokenConfigured, /no service token/);
  process.env.VAULT_TOKEN_COMMAND = "true";
  assert.doesNotThrow(assertTokenConfigured);
});

test("explains the failure when no token is stored yet", async () => {
  await assert.rejects(getField("residenza.comune"), (e: Error) => {
    assert.match(e.message, /no service token \(VAULT_TOKEN_COMMAND failed: .*No such file/);
    assert.match(e.message, /--keychain/);
    return true;
  });
  assert.equal(seen.length, 0, "no request without a token");
});

test("reports a command that prints nothing", async () => {
  await writeFile(tokenFile, "\n");
  await assert.rejects(getField("residenza.comune"), /VAULT_TOKEN_COMMAND printed nothing/);
});

test("uses a token stored mid-session", async () => {
  tokens.set("aaaa", "residenza.");
  await writeFile(tokenFile, "aaaa\n");
  assert.equal((await getField("residenza.comune")).value, "value-of-residenza.comune");
});

test("switches to a replaced token while the old one is still valid", async () => {
  tokens.set("aaaa", "residenza.");
  tokens.set("bbbb", "identity.");
  await writeFile(tokenFile, "aaaa\n");
  await getField("residenza.comune");
  await writeFile(tokenFile, "bbbb\n");
  assert.equal((await getField("identity.codice_fiscale")).value, "value-of-identity.codice_fiscale");
  assert.deepEqual(seen, ["aaaa", "bbbb"]);
});

test("rejects malformed command output without getting stuck on it", async () => {
  tokens.set("aaaa", "residenza.");
  await writeFile(tokenFile, "warning: something\naaaa\n");
  await assert.rejects(getField("residenza.comune"), /VAULT_TOKEN_COMMAND output is not a valid token/);
  assert.equal(seen.length, 0);
  await writeFile(tokenFile, "aaaa\n");
  assert.equal((await getField("residenza.comune")).value, "value-of-residenza.comune");
});

test("a revoked token fails with one request and Keychain advice", async () => {
  await writeFile(tokenFile, "revoked\n");
  await assert.rejects(getField("residenza.comune"), (e: Error) => {
    assert.match(e.message, /invalid, expired or revoked/);
    assert.match(e.message, /--keychain/);
    return true;
  });
  assert.equal(seen.length, 1);
});

test("VAULT_TOKEN wins over the command, and errors point at it", async () => {
  process.env.VAULT_TOKEN = "stale";
  process.env.VAULT_TOKEN_COMMAND = "exit 1"; // would fail if it ran
  await assert.rejects(getField("residenza.comune"), (e: Error) => {
    assert.match(e.message, /invalid, expired or revoked/);
    assert.match(e.message, /set it as VAULT_TOKEN/);
    assert.doesNotMatch(e.message, /--keychain/);
    return true;
  });
  assert.deepEqual(seen, ["stale"]);
});
