import { test, before, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir, userInfo } from "node:os";
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
  delete process.env.VAULT_KEYCHAIN_SERVICE;
  process.env.VAULT_TOKEN_COMMAND = `cat '${tokenFile}'`;
  await rm(tokenFile, { force: true });
});

test("refuses to start with no token source at all", () => {
  delete process.env.VAULT_TOKEN_COMMAND;
  assert.throws(assertTokenConfigured, /no service token/);
  process.env.VAULT_TOKEN_COMMAND = "true";
  assert.doesNotThrow(assertTokenConfigured);
  delete process.env.VAULT_TOKEN_COMMAND;
  process.env.VAULT_KEYCHAIN_SERVICE = "pvault-mcp";
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
  await assert.rejects(getField("residenza.comune"), /VAULT_TOKEN_COMMAND output is empty/);
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

test("points to Touch ID before asking for a new token", async () => {
  const touchIDFirst = (e: Error) => {
    const touchID = e.message.indexOf("pvault touchid status");
    assert.ok(touchID >= 0, `no Touch ID advice in: ${e.message}`);
    assert.ok(touchID < e.message.indexOf("create-service-token"), "Touch ID comes first");
    return true;
  };
  await assert.rejects(getField("residenza.comune"), touchIDFirst); // no token stored
  await writeFile(tokenFile, "revoked\n");
  await assert.rejects(getField("residenza.comune"), touchIDFirst); // expired or revoked
  process.env.VAULT_TOKEN = "stale";
  await assert.rejects(getField("residenza.comune"), touchIDFirst);
});

test("reports runaway command output as too large, not a timeout", async () => {
  process.env.VAULT_TOKEN_COMMAND = "node -e \"process.stdout.write('a'.repeat(100000))\"";
  await assert.rejects(getField("residenza.comune"), (e: Error) => {
    assert.match(e.message, /printed more than 64 KB/);
    assert.doesNotMatch(e.message, /timed out/);
    return true;
  });
});

test("names the exact Keychain service in its advice", async () => {
  const service = `pvault-test-missing-${randomBytes(4).toString("hex")}`;
  process.env.VAULT_KEYCHAIN_SERVICE = service;
  process.env.VAULT_TOKEN_COMMAND = "exit 1"; // the Keychain service wins over the command
  await assert.rejects(getField("residenza.comune"), (e: Error) => {
    assert.match(e.message, new RegExp(`--keychain ${service}\``));
    assert.match(e.message, /nothing stored in Keychain service|needs macOS/);
    assert.doesNotMatch(e.message, /VAULT_TOKEN_COMMAND/);
    return true;
  });
  assert.equal(seen.length, 0);
});

test("rejects an invalid Keychain service name", async () => {
  process.env.VAULT_KEYCHAIN_SERVICE = "-s";
  await assert.rejects(getField("residenza.comune"), /not a valid Keychain service name/);
});

// Touches the real login Keychain: PVAULT_KEYCHAIN_TEST=1 npm test
test("reads the token from the Keychain for this user", {
  skip: process.platform !== "darwin" || process.env.PVAULT_KEYCHAIN_TEST !== "1",
}, async () => {
  const service = `pvault-test-${randomBytes(4).toString("hex")}`;
  const account = userInfo().username;
  const security = (...args: string[]) => execFileSync("/usr/bin/security", args, { stdio: "ignore" });
  security("add-generic-password", "-a", account, "-s", service, "-w", "cccc");
  try {
    tokens.set("cccc", "residenza.");
    process.env.VAULT_KEYCHAIN_SERVICE = service;
    assert.equal((await getField("residenza.comune")).value, "value-of-residenza.comune");
    assert.deepEqual(seen, ["cccc"]);
  } finally {
    security("delete-generic-password", "-a", account, "-s", service);
  }
});

test("refuses field IDs that would leave /vault/fields/", async () => {
  tokens.set("t-scope", "identity.");
  process.env.VAULT_TOKEN = "t-scope";
  for (const id of ["identity./../../context", "identity./../spid.password", "identity.a/b", "identity.a?x", "noDot",
    "leo:/../context.x", "a:b:c.d", "io:identity.first_name", "Leo:identity.first_name", ":identity.first_name", "leo:"]) {
    seen.length = 0;
    await assert.rejects(getField(id), /invalid field ID/);
    assert.equal(seen.length, 0, `${id} reached the server`);
  }
  assert.equal((await getField("identity.first_name")).value, "value-of-identity.first_name");
  tokens.set("t-scope", "leo:identity.");
  assert.equal((await getField("leo:identity.first_name")).value, "value-of-leo:identity.first_name");
});
