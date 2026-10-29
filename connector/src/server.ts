import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { Entry, Registry, Status } from "./registry.js";

export const INSTRUCTIONS = `Napoli: which Italian government pratiche can be prepared automatically, starting with the Comune di Napoli's SUE and SUAP on impresainungiorno, and what the law asks for each.

Statuses: available = run on the real portal and unchanged since; paused = the portal changed, refused until it's mapped again; mapped = steps exist but haven't been run on the portal yet, so it's coming soon: don't run it for a client; guide = the library says what it needs and where, but it can't be filled automatically; coming = being worked on.

Rules:
- Send this server no personal data: no names, codici fiscali, addresses or documents of anyone. Describe a case by its facts only (the works, the activity, the zone, the size).
- Filling a pratica needs the Portali plugin in Cowork, with the client's data in the user's own folder. This server only says what exists and how.
- Never tick a declaration, sign, pay or send for the user: those steps are theirs.
- Answer about the law only from get_procedure, naming the sources it cites; say so when the library doesn't cover something.`;

const ORDER: Record<Status, number> = { available: 0, paused: 1, mapped: 2, coming: 3, guide: 4 };
const STATUSES = ["available", "paused", "mapped", "guide", "coming"] as const;
// Words too common in Italian requests to tell pratiche apart.
const STOP = new Set(["per", "una", "uno", "del", "della", "dei", "delle", "degli", "con", "che", "nel", "nella", "alla",
  "alle", "sul", "sulla", "gli", "come", "dove", "devo", "fare", "voglio", "mio", "mia", "suo", "sua", "the", "and", "for",
  "dopo", "prima", "anche", "cosa", "serve", "posso", "quale", "quali", "bisogna", "cliente", "clienti"]);

function ok(value: unknown): CallToolResult {
  return { content: [{ type: "text", text: JSON.stringify(value, null, 1) }] };
}

function err(message: string): CallToolResult {
  return { content: [{ type: "text", text: message }], isError: true };
}

function fold(s: string): string {
  return s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
}

function words(s: string): string[] {
  return fold(s).split(/[^a-z0-9]+/).filter((w) => w.length > 2 && !STOP.has(w));
}

function comuneCode(reg: Registry, comune?: string): string | undefined {
  if (!comune) return undefined;
  const found = Object.entries(reg.comuni).find(([code, name]) => fold(code) === fold(comune) || fold(name) === fold(comune));
  return found?.[0] ?? comune.toUpperCase();
}

function matches(e: Entry, comune?: string, avatar?: string): boolean {
  // A national procedure (IT) applies in every comune.
  return (!comune || e.comune === comune || e.comune === "IT") && (!avatar || e.avatars.length === 0 || e.avatars.includes(fold(avatar)));
}

function brief(e: Entry) {
  return { name: e.name, procedure: e.procedure, title: e.title, status: e.status, sportello: e.sportello,
    comune: e.comune, last_run: e.last_run ?? undefined, expected: e.expected };
}

/** How strongly one of a query's words is in a text's words: whole (2), or sharing all but
 *  its ending (1: "proroga" and "proroghe", "agibile" and "agibilita"). */
function strength(w: string, have: Set<string>): number {
  if (have.has(w)) return 2;
  return w.length >= 5 && [...have].some((h) => h.startsWith(w.slice(0, -2))) ? 1 : 0;
}

/** Entries ranked by the query's words in their title (weighed most), their library entry
 *  (its other names for the procedure) and their notes, with the words each matched. Those
 *  within reach of the best are kept: a case can need two pratiche ("ristorante con dehors"). */
export function find(reg: Registry, query: string, comune?: string, avatar?: string): Array<Entry & { score: number; matched: string[] }> {
  const want = [...new Set(words(query))];
  const ranked = reg.entries
    .filter((e) => matches(e, comune, avatar))
    .map((e) => {
      const fields: Array<[number, Set<string>]> = [
        [3, new Set(words(`${e.title ?? ""} ${e.procedure ?? ""}`))],
        [2, new Set(words(e.procedure ? reg.procedures.get(e.procedure)?.words ?? "" : ""))],
        [1, new Set(words(e.name ? reg.catalog.get(e.name)?.notes ?? "" : ""))],
      ];
      let score = 0;
      const matched: string[] = [];
      for (const w of want) {
        const best = Math.max(...fields.map(([weight, have]) => weight * strength(w, have)));
        score += best;
        if (best > 0) matched.push(w);
      }
      return { ...e, score, matched };
    })
    .filter((e) => e.score > 0)
    .sort((a, b) => b.score - a.score || ORDER[a.status] - ORDER[b.status]);
  const top = ranked[0]?.score ?? 0;
  return ranked.filter((e) => e.score >= top * 0.4);
}

/** The query's words no entry mentions anywhere: what the registry doesn't cover. */
export function unknownWords(reg: Registry, query: string): string[] {
  const all = new Set(reg.entries.flatMap((e) => words(`${e.title ?? ""} ${e.procedure ?? ""} ${
    e.procedure ? reg.procedures.get(e.procedure)?.words ?? "" : ""} ${e.name ? reg.catalog.get(e.name)?.notes ?? "" : ""}`)));
  return [...new Set(words(query))].filter((w) => strength(w, all) === 0);
}

export function createServer(reg: Registry): McpServer {
  const server = new McpServer({ name: "portali", version: reg.pluginVersion }, { instructions: INSTRUCTIONS });
  const readOnly = { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false };

  server.registerTool(
    "coverage",
    {
      title: "What's supported",
      description: "What can be prepared today and what's coming, by comune and by who files it (architetto, geometra, ingegnere, impresa, commercialista). Use it when the user asks what this can do, or before promising anything. Counts by status, then each pratica with its status and the date it last ran on the real portal.",
      inputSchema: {
        comune: z.string().max(60).optional().describe("A comune's name or Belfiore code (Napoli, F839); omit for all"),
        avatar: z.string().max(30).optional().describe("Who files: architetto, geometra, ingegnere, impresa, commercialista"),
        status: z.enum(STATUSES).optional(),
      },
      annotations: { ...readOnly, title: "What's supported" },
    },
    async ({ comune, avatar, status }) => {
      const code = comuneCode(reg, comune);
      const list = reg.entries.filter((e) => matches(e, code, avatar) && (!status || e.status === status))
        .sort((a, b) => ORDER[a.status] - ORDER[b.status] || (a.title ?? "").localeCompare(b.title ?? ""));
      const counts = Object.fromEntries(STATUSES.map((s) => [s, list.filter((e) => e.status === s).length]));
      return ok({ plugin_version: reg.pluginVersion, as_of: reg.built, comuni: reg.comuni, counts, pratiche: list.map(brief) });
    }
  );

  server.registerTool(
    "find_pratica",
    {
      title: "Find the pratica for a case",
      description: "The pratiche that fit a case, best first, each with its status. Describe the case by its facts only, never by who it's for: \"lavori interni non strutturali, nuovo bagno\", \"apertura ristorante con dehors\", \"proroga fine lavori\". Then get_pratica for one that can be filled, or get_procedure for what the law asks.",
      inputSchema: {
        query: z.string().min(3).max(300),
        comune: z.string().max(60).optional(),
        avatar: z.string().max(30).optional(),
      },
      annotations: { ...readOnly, title: "Find the pratica for a case" },
    },
    async ({ query, comune, avatar }) => {
      const found = find(reg, query, comuneCode(reg, comune), avatar).slice(0, 8);
      const unknown = unknownWords(reg, query);
      const note = !found.length ? "Nothing in the registry matches: this case isn't covered yet. Say so; don't guess a pratica."
        : unknown.length ? `No pratica here mentions ${unknown.join(", ")}: if that's what the case is about, it isn't covered yet.` : undefined;
      return ok({ found: found.map((e) => ({ ...brief(e), library: e.library, matched: e.matched })), unknown_words: unknown, note });
    }
  );

  server.registerTool(
    "get_pratica",
    {
      title: "One pratica, and whether your copy is current",
      description: "A pratica's status, the cases it refuses, who its roles are, the questions pre-flight asks and the steps left to the user. Pass local_sha256 (the plugin's catalog.json steps_sha256 for it) to learn whether the plugin's copy of its steps file is current; include_steps returns the current steps file, only when the plugin's is missing or stale (it's tens of KB).",
      inputSchema: {
        name: z.string().regex(/^[a-z0-9-]{1,64}$/),
        local_sha256: z.string().regex(/^[0-9a-f]{64}$/).optional(),
        include_steps: z.boolean().optional(),
      },
      annotations: { ...readOnly, title: "One pratica" },
    },
    async ({ name, local_sha256, include_steps }) => {
      const e = reg.entries.find((x) => x.name === name);
      const c = reg.catalog.get(name);
      if (!e || !c) return err(`no pratica ${name}: find_pratica or coverage lists them`);
      const steps = JSON.parse(reg.steps.get(name)!) as { asks?: Record<string, { question?: string }>; yours?: string[] };
      const copy = !local_sha256 ? "unknown" : local_sha256 === c.steps_sha256 ? "current" : "stale";
      const result: CallToolResult = ok({
        ...brief(e), changed: e.changed, refuses: c.refuses, notes: c.notes, library: e.library,
        asks: Object.entries(steps.asks ?? {}).map(([key, a]) => ({ key, question: a.question })),
        yours: steps.yours ?? [], steps_sha256: c.steps_sha256, copy,
        note: copy === "stale" ? "The plugin's steps file for this pratica is out of date: use the one include_steps returns, and tell the user to update the plugin." : undefined,
      });
      if (include_steps) result.content.push({ type: "text", text: reg.steps.get(name)! });
      return result;
    }
  );

  server.registerTool(
    "get_procedure",
    {
      title: "What the law asks for a procedure",
      description: "The pratiche-napoli library's entry for a procedure, as YAML: who it's for, documents, costs, deadlines, offices, steps, and the source of each fact. Use its procedure id from find_pratica or coverage (e.g. napoli/comune/cila-lavori-edilizi).",
      inputSchema: { procedure: z.string().regex(/^[a-z0-9-]+(\/[a-z0-9-]+){1,3}$/) },
      annotations: { ...readOnly, title: "What the law asks" },
    },
    async ({ procedure }) => {
      const p = reg.procedures.get(procedure);
      if (!p) return err(`the library has no entry for ${procedure} yet: say so rather than answering from memory`);
      return { content: [{ type: "text", text: p.text }] };
    }
  );

  return server;
}
