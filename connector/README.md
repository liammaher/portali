# The Portali connector

A remote MCP server, read-only and without sign-in, that the Portali plugin brings: what
pratiche can be prepared, by comune and by who files them, and the law behind each
(`docs/design/packaging.md`). It holds and receives no one's data: its tools take a case's
facts, and filling runs in the plugin, on the user's computer.

It serves the folder `plugin/build.py` writes (`coverage.json`, `catalog.json`, the steps
files, the library), so what it says is supported is what the plugin runs.

| Tool | What it answers |
|---|---|
| `coverage` | counts by status and every pratica, by comune and avatar |
| `find_pratica` | the pratiche that fit a case, best first, and the words nothing covers |
| `get_pratica` | its status, refusals, questions and the user's steps; whether the plugin's steps file is current (`local_sha256`), and the current one (`include_steps`) |
| `get_procedure` | the library's entry for a procedure, with its sources |

Statuses come from the repo, never by hand: `available` (run on the portal, unchanged
since), `paused` (the drift check found it changed), `mapped` (steps, no run yet), `guide`
(library only) and `coming` (`plugin/coverage.yaml`).

```sh
npm install && npm run build
REGISTRY_DIR=../plugin/dist/portali/skills/portali-pratica PORT=8080 npm start   # POST /mcp, GET /healthz
npm test                         # REGISTRY_DIR set: also checks the real build's files
```

Stateless (a server per request, nothing kept about a caller); a request with an Origin
header passes only from `ALLOWED_ORIGINS` (default claude.ai, claude.com), as Claude's own
requests come from Anthropic's cloud with none. Build the plugin with
`--connector https://<host>/mcp` once it's deployed.
