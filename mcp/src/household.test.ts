import { test } from "node:test";
import assert from "node:assert/strict";
import { filterHousehold } from "./household.js";
import type { Household } from "./client.js";

const household: Household = {
  intestatario_scheda: "io",
  members: [
    { slug: "giulia", famiglia: { relazione: "coniuge" }, first_name: "Giulia", relazione_label: "moglie", anpr_relazione: "2", fields: 3, documents: 0 },
    { slug: "leo", famiglia: { relazione: "figlio", genitori: "io,giulia" }, first_name: "Leo", age: 6, minor: true, anpr_relazione: "3", relazione_label: "figlio", fields: 5, documents: 1 },
  ],
};

test("full scope keeps everything", () => {
  assert.deepEqual(filterHousehold(household, "*"), household);
});

test("owner scopes reach no member", () => {
  assert.deepEqual(filterHousehold(household, "identity.*,famiglia.*"), { intestatario_scheda: "io", members: [] });
});

test("a member scope reaches only that member", () => {
  assert.deepEqual(filterHousehold(household, "leo:*").members, [household.members[1]]);
  assert.deepEqual(filterHousehold(household, "leo:cie.*"), { members: [{ slug: "leo" }] });
});

test("values only from fields the scope reaches", () => {
  const out = filterHousehold(household, "*:famiglia.*");
  assert.deepEqual(out.members[1], { slug: "leo", famiglia: { relazione: "figlio", genitori: "io,giulia" }, anpr_relazione: "3" });
  assert.equal(out.intestatario_scheda, "io");
  const named = filterHousehold(household, "*:identity.*,*:famiglia.relazione");
  assert.deepEqual(named.members[0], { slug: "giulia", famiglia: { relazione: "coniuge" }, first_name: "Giulia", anpr_relazione: "2", relazione_label: "moglie" });
});
