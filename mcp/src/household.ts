import type { Household, MemberInfo } from "./client.js";
import { scopeAllows, scopeAllowsMember, scopeAllowsWholeMember } from "./scope.js";

/**
 * Limits a household listing to VAULT_SCOPE, like every other tool: a member
 * shows only if the scope reaches some of their data, and each value only if
 * the scope reaches the field it comes from (age from identity.date_of_birth,
 * the gendered label from identity.sex too).
 */
export function filterHousehold(h: Household, scope: string): Household {
  const members = h.members
    .filter((m) => scopeAllowsMember(scope, m.slug))
    .map((m) => {
      const can = (local: string) => scopeAllows(scope, `${m.slug}:${local}`);
      const out: MemberInfo = { slug: m.slug };
      const famiglia = Object.fromEntries(Object.entries(m.famiglia ?? {}).filter(([k]) => can(`famiglia.${k}`)));
      if (Object.keys(famiglia).length > 0) out.famiglia = famiglia;
      if (can("identity.first_name") && m.first_name) out.first_name = m.first_name;
      if (can("identity.last_name") && m.last_name) out.last_name = m.last_name;
      if (can("identity.date_of_birth") && m.age !== undefined) {
        out.age = m.age;
        out.minor = m.minor;
      }
      if (can("famiglia.relazione")) {
        out.anpr_relazione = m.anpr_relazione;
        if (can("identity.sex")) out.relazione_label = m.relazione_label;
      }
      if (scopeAllowsWholeMember(scope, m.slug)) {
        out.fields = m.fields;
        out.documents = m.documents;
      }
      return out;
    });
  const result: Household = { members };
  if (h.intestatario_scheda && scopeAllows(scope, "famiglia.intestatario_scheda")) {
    result.intestatario_scheda = h.intestatario_scheda;
  }
  return result;
}
