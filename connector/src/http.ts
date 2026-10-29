import { createServer as createHttpServer, type IncomingMessage, type Server } from "node:http";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import type { Registry } from "./registry.js";
import { createServer } from "./server.js";

// Claude reaches a remote connector from Anthropic's cloud, with no Origin header; a browser
// page always sends one, so only Claude's own origins pass (no DNS rebinding, no other site).
export const DEFAULT_ORIGINS = ["https://claude.ai", "https://claude.com"];
const MAX_BODY = 1 << 20;

async function body(req: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of req) {
    size += (chunk as Buffer).length;
    if (size > MAX_BODY) throw new Error("request too large");
    chunks.push(chunk as Buffer);
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8"));
}

/** Stateless MCP over HTTP: a server per request, so nothing about a caller is kept. */
export function serve(reg: Registry, port: number, origins = DEFAULT_ORIGINS): Promise<Server> {
  const http = createHttpServer(async (req, res) => {
    const path = new URL(req.url ?? "/", "http://x").pathname;
    if (path === "/healthz") {
      res.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ ok: true, version: reg.pluginVersion, built: reg.built }));
      return;
    }
    if (path !== "/mcp") {
      res.writeHead(404).end();
      return;
    }
    const origin = req.headers.origin;
    if (origin && !origins.includes(origin)) {
      res.writeHead(403).end();
      return;
    }
    if (req.method !== "POST") {
      // No sessions, so no stream to resume (GET) or end (DELETE).
      res.writeHead(405, { Allow: "POST" }).end();
      return;
    }
    let parsed: unknown;
    try {
      parsed = await body(req);
    } catch (e) {
      res.writeHead(400, { "Content-Type": "application/json" })
        .end(JSON.stringify({ jsonrpc: "2.0", error: { code: -32700, message: (e as Error).message }, id: null }));
      return;
    }
    const server = createServer(reg);
    const transport = new StreamableHTTPServerTransport({ sessionIdGenerator: undefined, enableJsonResponse: true });
    res.on("close", () => {
      void transport.close();
      void server.close();
    });
    await server.connect(transport);
    await transport.handleRequest(req, res, parsed);
  });
  return new Promise((resolve) => http.listen(port, () => resolve(http)));
}
