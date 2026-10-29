import { serve, DEFAULT_ORIGINS } from "./http.js";
import { loadRegistry } from "./registry.js";

// REGISTRY_DIR is the plugin's built skill folder (plugin/dist/portali/skills/portali-pratica).
const dir = process.env.REGISTRY_DIR;
if (!dir) {
  console.error("Portali connector: set REGISTRY_DIR to the built plugin's portali-pratica folder (plugin/build.py writes it)");
  process.exit(1);
}
const reg = await loadRegistry(dir);
const port = Number(process.env.PORT ?? 8080);
const origins = process.env.ALLOWED_ORIGINS ? process.env.ALLOWED_ORIGINS.split(",") : DEFAULT_ORIGINS;
await serve(reg, port, origins);
console.error(`Portali connector ${reg.pluginVersion} (${reg.built}): ${reg.entries.length} entries on :${port}/mcp`);
