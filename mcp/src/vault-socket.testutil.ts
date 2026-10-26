import { createServer, type RequestListener, type Server } from "node:http";
import { mkdir, mkdtemp } from "node:fs/promises";
import { join } from "node:path";

/**
 * A fake vault server on <dir>/run/pvault.sock, as pvault serve makes it. The
 * dir is under /tmp: a socket path is limited to 104 bytes, more than macOS's
 * own temp folder leaves.
 */
export async function fakeVaultSocket(handler: RequestListener): Promise<{ dir: string; server: Server }> {
  const dir = await mkdtemp("/tmp/vmcp-");
  await mkdir(join(dir, "run"), { mode: 0o700 });
  const server = createServer(handler);
  await new Promise<void>((r) => server.listen(join(dir, "run", "pvault.sock"), r));
  return { dir, server };
}
