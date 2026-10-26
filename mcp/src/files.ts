import { execFile } from "node:child_process";
import { open, unlink } from "node:fs/promises";
import { basename, extname, join } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

/** Bidirectional-text controls, which can make "fdp.exe" display as "exe.pdf". */
const BIDI_CONTROLS = /[\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069]/g;
/** In UTF-8 bytes, as the vault counts them (it refuses names over 255), with room for " (999)". */
const MAX_NAME_BYTES = 200;

/** A plain file name: no folders, no leading dot, no control characters, extension kept. */
export function safeFilename(name: string | undefined, fallback: string): string {
  let cleaned = basename((name ?? "").replace(/[\\/]/g, "_"))
    // C0, DEL and C1: the vault refuses every Unicode control character.
    .replace(/[\u0000-\u001f\u007f-\u009f]/g, "")
    .replace(BIDI_CONTROLS, "")
    .replace(/^[\s.]+/, "")
    .trim();
  if (Buffer.byteLength(cleaned) > MAX_NAME_BYTES) {
    const ext = [...extname(cleaned)].slice(0, 20).join("");
    let stem = "";
    for (const ch of cleaned.slice(0, cleaned.length - extname(cleaned).length)) {
      if (Buffer.byteLength(stem + ch + ext) > MAX_NAME_BYTES) break;
      stem += ch;
    }
    cleaned = stem + ext;
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

/**
 * Marks a file that came from outside senders as downloaded (macOS), so
 * Gatekeeper warns before it is opened, as for a browser download. A file
 * that can't be marked is removed rather than left unmarked.
 */
export async function quarantine(path: string, agent = "pvault-mcp"): Promise<void> {
  if (process.platform !== "darwin") return;
  const value = `0081;${Math.floor(Date.now() / 1000).toString(16)};${agent};`;
  try {
    await execFileAsync("/usr/bin/xattr", ["-w", "com.apple.quarantine", value, path]);
  } catch (e) {
    await unlink(path).catch(() => {});
    throw new Error(`could not mark ${basename(path)} as downloaded, so it was not saved: ${(e as Error).message}`);
  }
}
