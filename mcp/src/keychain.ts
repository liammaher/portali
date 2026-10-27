import { execFile } from "node:child_process";
import { existsSync } from "node:fs";
import { win32 } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

/** macOS's Keychain tool, by absolute path so nothing earlier in PATH can stand in for it. */
export const SECURITY_PATH = "/usr/bin/security";
/**
 * Exit status for a missing item: `security find-generic-password`'s
 * (errSecItemNotFound), which `pvault credential read` uses too.
 */
export const SECURITY_ITEM_NOT_FOUND = 44;
/** Same rule as `pvault create-service-token --keychain`. */
export const KEYCHAIN_SERVICE_PATTERN = /^[A-Za-z0-9._][A-Za-z0-9._-]{0,63}$/;

export const LOOKUP_TIMEOUT_MS = 10_000;
export const LOOKUP_MAX_BYTES = 64 * 1024;

/** Where secrets are kept on this system, and what one entry is called, for messages. */
export const SECRET_STORE = process.platform === "win32" ? "Credential Manager" : "Keychain";
export const SECRET_KIND = process.platform === "win32" ? "Credential Manager credential" : "Keychain service";

/**
 * On Windows, pvault.exe reads Credential Manager for this server (Node has no
 * API for it): PVAULT_BIN, an absolute path, else pvault.exe in an absolute
 * PATH folder. Never the current folder, where a planted pvault.exe would win.
 */
export function pvaultPath(env: NodeJS.ProcessEnv = process.env, exists: (p: string) => boolean = existsSync): string | undefined {
  const bin = env.PVAULT_BIN?.trim();
  if (bin) return win32.isAbsolute(bin) ? bin : undefined;
  for (const entry of (env.PATH ?? env.Path ?? "").split(";")) {
    const dir = entry.trim().replace(/^"(.*)"$/, "$1"); // Windows allows quoted entries
    if (!dir || !win32.isAbsolute(dir)) continue;
    const p = win32.join(dir, "pvault.exe");
    if (exists(p)) return p;
  }
  return undefined;
}

/** What to tell the user when the secret store's tool isn't there. */
export function storeToolMissing(variable: string): string {
  return process.platform === "win32"
    ? `pvault.exe not found on PATH (or set PVAULT_BIN to its full path); ${variable} reads Windows Credential Manager through it`
    : `${SECURITY_PATH} not found; ${variable} needs macOS or Windows`;
}

/**
 * Reads a generic password: from the login Keychain, or on Windows from
 * Credential Manager through `pvault credential read`. Errors are the raw
 * execFile errors (code 44 = not found, "ENOENT" = no tool) so callers can
 * word them for their own setup.
 */
export async function findGenericPassword(service: string, account: string): Promise<string> {
  if (process.platform === "win32") {
    const bin = process.env.PVAULT_BIN?.trim();
    if (bin && !win32.isAbsolute(bin)) {
      throw Object.assign(new Error("PVAULT_BIN is not a full path"), {
        stderr: `PVAULT_BIN must be the full path of pvault.exe, not "${bin}"`,
      });
    }
    const pvault = pvaultPath();
    if (!pvault) throw Object.assign(new Error("pvault.exe not found"), { code: "ENOENT" });
    const { stdout } = await execFileAsync(pvault, ["credential", "read", service, "--account", account], {
      timeout: LOOKUP_TIMEOUT_MS, maxBuffer: LOOKUP_MAX_BYTES, windowsHide: true,
    });
    return stdout.trim();
  }
  const { stdout } = await execFileAsync(
    SECURITY_PATH,
    ["find-generic-password", "-a", account, "-s", service, "-w"],
    { timeout: LOOKUP_TIMEOUT_MS, maxBuffer: LOOKUP_MAX_BYTES },
  );
  return stdout.trim();
}
