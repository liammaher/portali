import type { Household, MemberInfo } from "./client.js";
import { scopeAllows, scopeAllowsPerson, scopeAllowsWholePerson } from "./scope.js";

/**
 * Limits a listing of the vault's people to VAULT_SCOPE, like every other
 * tool: a person shows only if the scope reaches some of their data, and each
 * value only if the scope reaches the field it comes from (famiglia.* or
 * ruolo.*, age from identity.date_of_birth, the gendered label from
 * identity.sex too, a company's name from business.denominazione).
 */
export function filterHousehold(h: Household, scope: string): Household {
  const members = h.members
    .filter((m) => scopeAllowsPerson(scope, m.slug))
    .map((m) => {
      const can = (local: string) => scopeAllows(scope, `${m.slug}:${local}`);
      const out: MemberInfo = { slug: m.slug };
      const famiglia = Object.fromEntries(Object.entries(m.famiglia ?? {}).filter(([k]) => can(`famiglia.${k}`)));
      if (Object.keys(famiglia).length > 0) out.famiglia = famiglia;
      const ruolo = Object.fromEntries(Object.entries(m.ruolo ?? {}).filter(([k]) => can(`ruolo.${k}`)));
      if (Object.keys(ruolo).length > 0) out.ruolo = ruolo;
      if (can("identity.first_name") && m.first_name) out.first_name = m.first_name;
      if (can("identity.last_name") && m.last_name) out.last_name = m.last_name;
      if (can("business.denominazione") && m.denominazione) out.denominazione = m.denominazione;
      if (can("identity.date_of_birth") && m.age !== undefined) {
        out.age = m.age;
        out.minor = m.minor;
      }
      if (can("famiglia.relazione")) {
        if (m.anpr_relazione !== undefined) out.anpr_relazione = m.anpr_relazione;
        if (can("identity.sex") && m.relazione_label !== undefined) out.relazione_label = m.relazione_label;
      }
      if (scopeAllowsWholePerson(scope, m.slug)) {
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
