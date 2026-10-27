import { test, before, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir, userInfo } from "node:os";
import { join } from "node:path";
import { assertTokenConfigured, getField, serverPipe } from "./client.js";
import { fakeVaultSocket } from "./vault-socket.testutil.js";

// Fake vault: each valid token may read fields under one category prefix.
const tokens = new Map<string, string>();
const seen: string[] = [];
let server: Server;
let vaultDir: string;
let statusAddr = "127.0.0.1:7200";
let dir: string;
let tokenFile: string;

before(async () => {
  ({ dir: vaultDir, server } = await fakeVaultSocket((req, res) => {
    if (req.url === "/vault/status") {
      res.end(JSON.stringify({ locked: false, addr: statusAddr }));
      return;
    }
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
  }));
  process.env.VAULT_DIR = vaultDir;
  delete process.env.VAULT_ADDR;
  delete process.env.VAULT_NAME;
  dir = await mkdtemp(join(tmpdir(), "vault-mcp-test-"));
  tokenFile = join(dir, "token");
});

after(async () => {
  server.close();
  await rm(dir, { recursive: true, force: true });
  await rm(vaultDir, { recursive: true, force: true });
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

// While the vault is locked anything can listen on its port: the token goes
// only to the server's socket in the vault folder, never over TCP.
test("never sends the token to whatever answers on VAULT_ADDR", async () => {
  const heard: string[] = [];
  const squatter = createServer((req, res) => {
    heard.push(req.headers.authorization ?? "");
    res.end(JSON.stringify({ locked: false, addr: "127.0.0.1:0", id: "x", value: "fake" }));
  });
  await new Promise<void>((r) => squatter.listen(0, "127.0.0.1", r));
  const locked = await mkdtemp("/tmp/vmcp-locked-"); // a vault with no server: no socket
  process.env.VAULT_DIR = locked;
  process.env.VAULT_ADDR = `http://127.0.0.1:${(squatter.address() as AddressInfo).port}`;
  tokens.set("t-1", "identity.");
  await writeFile(tokenFile, "t-1\n");
  try {
    await assert.rejects(getField("identity.first_name"), /server not running/);
    assert.deepEqual(heard, []);
  } finally {
    squatter.close();
    process.env.VAULT_DIR = vaultDir;
    delete process.env.VAULT_ADDR;
    await rm(locked, { recursive: true, force: true });
  }
});

test("refuses a socket in a folder others can enter", async () => {
  tokens.set("t-1", "identity.");
  await writeFile(tokenFile, "t-1\n");
  await chmod(join(vaultDir, "run"), 0o755);
  try {
    await assert.rejects(getField("identity.first_name"), /refusing to send the token.*0700/);
    assert.equal(seen.length, 0);
  } finally {
    await chmod(join(vaultDir, "run"), 0o700);
  }
  assert.equal((await getField("identity.first_name")).value, "value-of-identity.first_name");
});

test("VAULT_ADDR must be the address of this vault's server", async () => {
  tokens.set("t-1", "identity.");
  await writeFile(tokenFile, "t-1\n");
  statusAddr = "127.0.0.1:7201";
  process.env.VAULT_ADDR = "http://127.0.0.1:7200";
  try {
    await assert.rejects(getField("identity.first_name"), /VAULT_ADDR http:\/\/127.0.0.1:7200 isn't the server of .* which listens on 127.0.0.1:7201/);
    assert.equal(seen.length, 0, "the token went to the wrong vault");
    process.env.VAULT_ADDR = "http://127.0.0.1:7201";
    assert.equal((await getField("identity.first_name")).value, "value-of-identity.first_name");
  } finally {
    statusAddr = "127.0.0.1:7200";
    delete process.env.VAULT_ADDR;
  }
});

test("VAULT_NAME means ~/.pvault-<name>", async () => {
  const home = await mkdtemp("/tmp/vmcp-home-");
  const studio = join(home, ".pvault-studio");
  await mkdir(studio);
  const { server: other } = await fakeVaultSocketAt(studio);
  const saved = { home: process.env.HOME, dir: process.env.VAULT_DIR };
  process.env.HOME = home;
  delete process.env.VAULT_DIR;
  process.env.VAULT_NAME = "studio";
  await writeFile(tokenFile, "t-1\n");
  try {
    assert.equal((await getField("business.denominazione")).value, "studio-business.denominazione");
    process.env.VAULT_NAME = "../x";
    await assert.rejects(getField("business.denominazione"), /isn't a vault name/);
  } finally {
    other.close();
    process.env.HOME = saved.home;
    process.env.VAULT_DIR = saved.dir;
    delete process.env.VAULT_NAME;
    await rm(home, { recursive: true, force: true });
  }
});

/** A second fake vault in an existing folder, answering every field. */
async function fakeVaultSocketAt(folder: string): Promise<{ server: Server }> {
  await mkdir(join(folder, "run"), { mode: 0o700 });
  const other = createServer((req, res) => {
    const id = decodeURIComponent((req.url ?? "").replace("/vault/fields/", ""));
    res.end(JSON.stringify({ id, value: `studio-${id}` }));
  });
  await new Promise<void>((r) => other.listen(join(folder, "run", "pvault.sock"), r));
  return { server: other };
}


// On Windows a pipe name counts only while the server holds its file open,
// which Windows shows by refusing to delete the file (EBUSY).
test("serverPipe trusts a name only while the server holds its file", async () => {
  const good = "\\\\.\\pipe\\pvault-" + "ab".repeat(16);
  const err = (code: string) => Object.assign(new Error(code), { code });
  const fs = (name: string, unlinkErr?: string) => ({
    readFile: async () => name + "\n",
    unlink: async () => { if (unlinkErr) throw err(unlinkErr); },
  });
  assert.equal(await serverPipe("run/pipe", fs(good, "EBUSY")), good);
  // The delete worked: the server is gone, and the name may be anyone's.
  await assert.rejects(serverPipe("run/pipe", fs(good)), /server not running/);
  await assert.rejects(serverPipe("run/pipe", fs(good, "ENOENT")), /server not running/);
  await assert.rejects(serverPipe("run/pipe", fs(good, "EPERM")), /refusing/);
  await assert.rejects(serverPipe("run/pipe", fs("\\\\.\\pipe\\other", "EBUSY")), /refusing/);
  await assert.rejects(serverPipe("run/pipe", fs(good + "\\x", "EBUSY")), /refusing/);
  await assert.rejects(serverPipe("run/pipe", { readFile: async () => { throw err("ENOENT"); }, unlink: async () => {} }), /server not running/);
});

// On Windows the token comes from Credential Manager through pvault.exe, which
// is found only by an absolute path: PVAULT_BIN, or a PATH folder, never the
// current folder.
test("pvaultPath finds pvault.exe only by an absolute path", async () => {
  const { pvaultPath } = await import("./keychain.js");
  const has = (...paths: string[]) => (p: string) => paths.includes(p);
  assert.equal(pvaultPath({ PVAULT_BIN: "C:\\pv\\pvault.exe" }, has()), "C:\\pv\\pvault.exe");
  assert.equal(pvaultPath({ PVAULT_BIN: "pvault.exe" }, has("pvault.exe")), undefined);
  assert.equal(pvaultPath({ PATH: ".;C:\\tools;C:\\bin" }, has(".\\pvault.exe", "C:\\bin\\pvault.exe")), "C:\\bin\\pvault.exe");
  assert.equal(pvaultPath({ Path: "C:\\bin" }, has("C:\\bin\\pvault.exe")), "C:\\bin\\pvault.exe");
  assert.equal(pvaultPath({ PATH: ";relative\\dir" }, has("relative\\dir\\pvault.exe")), undefined);
  assert.equal(pvaultPath({ PATH: '"C:\\Program Files\\pvault";C:\\bin' }, has("C:\\Program Files\\pvault\\pvault.exe")), "C:\\Program Files\\pvault\\pvault.exe");
});
