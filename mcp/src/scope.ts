/**
 * MCP-layer scope enforcement — mirrors internal/vault/scope.go.
 *
 * Patterns: "*" (all, every person), "category.*" (an owner category),
 * "category.field" (exact), "leo:category.*" / "leo:category.field" (another
 * person's, e.g. a family member), "*:category.*" (that category for the owner
 * and every person), "leo:*" (everything about a person). A pattern without a
 * person prefix never reaches a person's data. Cases shared with the Go tests live in
 * internal/vault/testdata/scopes.json.
 */

export function getScope(): string {
  return process.env.VAULT_SCOPE ?? "*";
}

const ID_PART = /^[A-Za-z0-9_-]+$/;
const PERSON_SLUG = /^[a-z][a-z0-9-]{0,31}$/;

/** The vault's owner when a value names a person; never a person prefix. */
export const OWNER_REF = "io";

export function validPersonSlug(slug: string): boolean {
  return slug !== OWNER_REF && PERSON_SLUG.test(slug);
}

/** Splits at a ':' before the first '.': "leo:cie.numero" → ["leo", "cie.numero", true]. */
function cutPerson(id: string): [string, string, boolean] {
  const i = id.indexOf(":");
  if (i < 0) return ["", id, false];
  const dot = id.indexOf(".");
  if (dot >= 0 && dot < i) return ["", id, false];
  return [id.slice(0, i), id.slice(i + 1), true];
}

/** The person's slug ("" for the owner) and the rest of a field ID or category. */
export function splitPerson(id: string): [string, string] {
  const [person, local] = cutPerson(id);
  return [person, local];
}

interface Pattern {
  all: boolean;
  person: string; // "" (owner), a slug, or "*"
  wholePerson: boolean;
  category: string;
  field: string; // "" for category.*
}

function parsePattern(p: string): Pattern | null {
  if (p === "*") return { all: true, person: "", wholePerson: false, category: "", field: "" };
  const sp: Pattern = { all: false, person: "", wholePerson: false, category: "", field: "" };
  const [person, local, prefixed] = cutPerson(p);
  if (prefixed) {
    if (person !== "*" && !validPersonSlug(person)) return null;
    sp.person = person;
    if (local === "*") {
      sp.wholePerson = true;
      return person === "*" ? null : sp;
    }
  }
  const dot = local.indexOf(".");
  if (dot < 0) return null;
  const category = local.slice(0, dot);
  const field = local.slice(dot + 1);
  if (!ID_PART.test(category)) return null;
  sp.category = category;
  if (field !== "*") {
    if (!ID_PART.test(field)) return null;
    sp.field = field;
  }
  return sp;
}

function patterns(scope: string): Pattern[] {
  return scope
    .split(",")
    .map((p) => parsePattern(p.trim()))
    .filter((p): p is Pattern => p !== null);
}

function allowsPerson(sp: Pattern, person: string): boolean {
  return sp.all || sp.person === "*" || sp.person === person;
}

function allowsCategory(sp: Pattern, person: string, category: string): boolean {
  return allowsPerson(sp, person) && (sp.all || sp.wholePerson || sp.category === category);
}

/** An ID without a category and field (no dot) is reached only by "*". */
export function scopeAllows(scope: string, fieldID: string): boolean {
  const [person, local] = cutPerson(fieldID);
  const dot = local.indexOf(".");
  const category = local.slice(0, dot);
  const field = local.slice(dot + 1);
  return patterns(scope).some(
    (sp) => sp.all || (dot >= 0 && allowsCategory(sp, person, category) && (sp.wholePerson || sp.field === "" || sp.field === field)),
  );
}

export function scopeAllowsCategory(scope: string, category: string): boolean {
  const [person, local] = cutPerson(category);
  return patterns(scope).some((sp) => allowsCategory(sp, person, local));
}

/** Whether a scope reaches all of a person's data ("leo:*" or "*"). */
export function scopeAllowsWholePerson(scope: string, person: string): boolean {
  return patterns(scope).some((sp) => sp.all || (sp.wholePerson && sp.person === person));
}

/** Whether a scope reaches any data of a person ("" is the owner). */
export function scopeAllowsPerson(scope: string, person: string): boolean {
  return patterns(scope).some((sp) => allowsPerson(sp, person));
}
