import { open } from "node:fs/promises";
import { basename, extname, join } from "node:path";

/** A plain file name: no folders, no leading dot, no control characters. */
export function safeFilename(name: string | undefined, fallback: string): string {
  const cleaned = basename((name ?? "").replace(/[\\/]/g, "_"))
    .replace(/[\u0000-\u001f\u007f]/g, "")
    .replace(/^\.+/, "")
    .trim()
    .slice(0, 200);
  return cleaned || fallback;
}

/** Creates dir/name (or "name (2).ext", ...) without ever replacing a file. */
export async function writeNewFile(dir: string, name: string, data: Buffer): Promise<string> {
  const ext = extname(name);
  const stem = name.slice(0, name.length - ext.length);
  for (let n = 1; n < 1000; n++) {
    const path = join(dir, n === 1 ? name : `${stem} (${n})${ext}`);
    let handle;
    try {
      handle = await open(path, "wx", 0o600);
    } catch (e) {
      if ((e as { code?: string }).code === "EEXIST") continue;
      throw e;
    }
    try {
      await handle.writeFile(data);
    } finally {
      await handle.close();
    }
    return path;
  }
  throw new Error(`too many files named like ${name} in ${dir}`);
}
