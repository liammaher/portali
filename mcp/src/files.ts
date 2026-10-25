import { open, unlink } from "node:fs/promises";
import { basename, extname, join } from "node:path";

/** Bidirectional-text controls, which can make "fdp.exe" display as "exe.pdf". */
const BIDI_CONTROLS = /[\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069]/g;
const MAX_NAME = 200;

/** A plain file name: no folders, no leading dot, no control characters, extension kept. */
export function safeFilename(name: string | undefined, fallback: string): string {
  let cleaned = basename((name ?? "").replace(/[\\/]/g, "_"))
    .replace(/[\u0000-\u001f\u007f]/g, "")
    .replace(BIDI_CONTROLS, "")
    .replace(/^[\s.]+/, "")
    .trim();
  if (cleaned.length > MAX_NAME) {
    const ext = extname(cleaned).slice(0, 20);
    cleaned = cleaned.slice(0, MAX_NAME - ext.length) + ext;
  }
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
    } catch (e) {
      await handle.close();
      await unlink(path).catch(() => {});
      throw e;
    }
    await handle.close();
    return path;
  }
  throw new Error(`too many files named like ${name} in ${dir}`);
}
