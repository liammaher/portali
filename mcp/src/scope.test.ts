import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { scopeAllows, scopeAllowsCategory, scopeAllowsMember, splitMember } from "./scope.js";

// The same cases as internal/vault/scope_test.go, so the two matchers agree.
const fixture = JSON.parse(
  readFileSync(new URL("../../internal/vault/testdata/scopes.json", import.meta.url), "utf8"),
) as {
  fields: { scope: string; id: string; want: boolean }[];
  categories: { scope: string; category: string; want: boolean }[];
  members: { scope: string; member: string; want: boolean }[];
};

test("scopeAllows matches the Go cases", () => {
  for (const c of fixture.fields) {
    assert.equal(scopeAllows(c.scope, c.id), c.want, `scopeAllows(${JSON.stringify(c.scope)}, ${JSON.stringify(c.id)})`);
  }
});

test("scopeAllowsCategory matches the Go cases", () => {
  for (const c of fixture.categories) {
    assert.equal(scopeAllowsCategory(c.scope, c.category), c.want, `scopeAllowsCategory(${JSON.stringify(c.scope)}, ${JSON.stringify(c.category)})`);
  }
});

test("scopeAllowsMember matches the Go cases", () => {
  for (const c of fixture.members) {
    assert.equal(scopeAllowsMember(c.scope, c.member), c.want, `scopeAllowsMember(${JSON.stringify(c.scope)}, ${JSON.stringify(c.member)})`);
  }
});

test("splitMember", () => {
  assert.deepEqual(splitMember("leo:cie.numero"), ["leo", "cie.numero"]);
  assert.deepEqual(splitMember("cie.numero"), ["", "cie.numero"]);
  assert.deepEqual(splitMember("cie.a:b"), ["", "cie.a:b"]);
});
