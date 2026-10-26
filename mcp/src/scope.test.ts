import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { scopeAllows, scopeAllowsCategory, scopeAllowsPerson, splitPerson } from "./scope.js";

// The same cases as internal/vault/scope_test.go, so the two matchers agree.
const fixture = JSON.parse(
  readFileSync(new URL("../../internal/vault/testdata/scopes.json", import.meta.url), "utf8"),
) as {
  fields: { scope: string; id: string; want: boolean }[];
  categories: { scope: string; category: string; want: boolean }[];
  people: { scope: string; person: string; want: boolean }[];
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

test("scopeAllowsPerson matches the Go cases", () => {
  for (const c of fixture.people) {
    assert.equal(scopeAllowsPerson(c.scope, c.person), c.want, `scopeAllowsPerson(${JSON.stringify(c.scope)}, ${JSON.stringify(c.person)})`);
  }
});

test("splitPerson", () => {
  assert.deepEqual(splitPerson("leo:cie.numero"), ["leo", "cie.numero"]);
  assert.deepEqual(splitPerson("cie.numero"), ["", "cie.numero"]);
  assert.deepEqual(splitPerson("cie.a:b"), ["", "cie.a:b"]);
});
