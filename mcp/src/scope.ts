/**
 * MCP-layer scope enforcement — mirrors internal/vault/scope.go.
 *
 * Patterns: "*" (all, every person), "category.*" (an owner category),
 * "category.field" (exact), "leo:category.*" / "leo:category.field" (a family
 * member's), "*:category.*" (that category for the owner and every member),
 * "leo:*" (everything about a member). A pattern without a member prefix never
 * reaches a member's data. Cases shared with the Go tests live in
 * internal/vault/testdata/scopes.json.
 */

export function getScope(): string {
  return process.env.VAULT_SCOPE ?? "*";
}

const ID_PART = /^[A-Za-z0-9_-]+$/;
const MEMBER_SLUG = /^[a-z][a-z0-9-]{0,31}$/;

/** The vault's owner when a value names a person; never a member prefix. */
export const OWNER_REF = "io";

export function validMemberSlug(slug: string): boolean {
  return slug !== OWNER_REF && MEMBER_SLUG.test(slug);
}

/** Splits at a ':' before the first '.': "leo:cie.numero" → ["leo", "cie.numero", true]. */
function cutMember(id: string): [string, string, boolean] {
  const i = id.indexOf(":");
  if (i < 0) return ["", id, false];
  const dot = id.indexOf(".");
  if (dot >= 0 && dot < i) return ["", id, false];
  return [id.slice(0, i), id.slice(i + 1), true];
}

/** The member's slug ("" for the owner) and the rest of a field ID or category. */
export function splitMember(id: string): [string, string] {
  const [member, local] = cutMember(id);
  return [member, local];
}

interface Pattern {
  all: boolean;
  member: string; // "" (owner), a slug, or "*"
  wholeMember: boolean;
  category: string;
  field: string; // "" for category.*
}

function parsePattern(p: string): Pattern | null {
  if (p === "*") return { all: true, member: "", wholeMember: false, category: "", field: "" };
  const sp: Pattern = { all: false, member: "", wholeMember: false, category: "", field: "" };
  const [member, local, prefixed] = cutMember(p);
  if (prefixed) {
    if (member !== "*" && !validMemberSlug(member)) return null;
    sp.member = member;
    if (local === "*") {
      sp.wholeMember = true;
      return member === "*" ? null : sp;
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

function allowsMember(sp: Pattern, member: string): boolean {
  return sp.all || sp.member === "*" || sp.member === member;
}

function allowsCategory(sp: Pattern, member: string, category: string): boolean {
  return allowsMember(sp, member) && (sp.all || sp.wholeMember || sp.category === category);
}

/** An ID without a category and field (no dot) is reached only by "*". */
export function scopeAllows(scope: string, fieldID: string): boolean {
  const [member, local] = cutMember(fieldID);
  const dot = local.indexOf(".");
  const category = local.slice(0, dot);
  const field = local.slice(dot + 1);
  return patterns(scope).some(
    (sp) => sp.all || (dot >= 0 && allowsCategory(sp, member, category) && (sp.wholeMember || sp.field === "" || sp.field === field)),
  );
}

export function scopeAllowsCategory(scope: string, category: string): boolean {
  const [member, local] = cutMember(category);
  return patterns(scope).some((sp) => allowsCategory(sp, member, local));
}

/** Whether a scope reaches all of a member's data ("leo:*" or "*"). */
export function scopeAllowsWholeMember(scope: string, member: string): boolean {
  return patterns(scope).some((sp) => sp.all || (sp.wholeMember && sp.member === member));
}

/** Whether a scope reaches any data of a member ("" is the owner). */
export function scopeAllowsMember(scope: string, member: string): boolean {
  return patterns(scope).some((sp) => allowsMember(sp, member));
}
