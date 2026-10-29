import { readFile, readdir } from "node:fs/promises";
import { join, relative } from "node:path";
import { parse } from "yaml";

// What plugin/build.py writes into the plugin's skill folder: the connector serves the
// same files, so what it says is supported is what the plugin can run.

export type Status = "available" | "paused" | "mapped" | "guide" | "coming";

export interface Entry {
  name: string | null;
  procedure?: string;
  title: string | null;
  comune: string;
  sportello: string | null;
  status: Status;
  avatars: string[];
  last_run?: string | null;
  changed?: string[];
  steps_sha256?: string;
  library: boolean;
  expected?: string;
}

export interface CatalogEntry {
  name: string;
  procedure: string;
  title: string | null;
  refuses: string[];
  notes: string;
  steps_sha256: string;
}

export interface Procedure {
  id: string;
  title: string | null;
  words: string;
  text: string;
}

export interface Registry {
  pluginVersion: string;
  built: string;
  comuni: Record<string, string>;
  entries: Entry[];
  catalog: Map<string, CatalogEntry>;
  steps: Map<string, string>;
  procedures: Map<string, Procedure>;
}

async function json<T>(path: string): Promise<T> {
  return JSON.parse(await readFile(path, "utf8")) as T;
}

async function yamlFiles(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const e of await readdir(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) out.push(...(await yamlFiles(p)));
    else if (e.name.endsWith(".yaml")) out.push(p);
  }
  return out;
}

function procedure(id: string, text: string): Procedure {
  // The words a search matches: title, headline, aka and summary. A file that doesn't
  // parse is still served whole; only its title is found by line.
  try {
    const doc = parse(text) as Record<string, unknown>;
    const aka = Array.isArray(doc.aka) ? doc.aka.join(" ") : "";
    const words = [doc.title, doc.headline, aka, doc.summary].filter((w): w is string => typeof w === "string").join(" ");
    return { id, title: typeof doc.title === "string" ? doc.title : null, words, text };
  } catch {
    const title = /^title: *(.+)$/m.exec(text)?.[1]?.trim() ?? null;
    return { id, title, words: title ?? "", text };
  }
}

export async function loadRegistry(dir: string): Promise<Registry> {
  const coverage = await json<{ plugin_version: string; built: string; comuni: Record<string, string>; pratiche: Entry[] }>(
    join(dir, "coverage.json"));
  const catalog = new Map((await json<CatalogEntry[]>(join(dir, "catalog.json"))).map((c) => [c.name, c]));
  const steps = new Map<string, string>();
  for (const name of catalog.keys()) steps.set(name, await readFile(join(dir, "pratiche", `${name}.json`), "utf8"));
  const procedures = new Map<string, Procedure>();
  const root = join(dir, "library", "procedures");
  for (const f of await yamlFiles(root)) {
    const id = relative(root, f).replace(/\.yaml$/, "").split("\\").join("/");
    procedures.set(id, procedure(id, await readFile(f, "utf8")));
  }
  return {
    pluginVersion: coverage.plugin_version, built: coverage.built, comuni: coverage.comuni,
    entries: coverage.pratiche, catalog, steps, procedures,
  };
}
