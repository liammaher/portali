import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

/** macOS's Keychain tool, by absolute path so nothing earlier in PATH can stand in for it. */
export const SECURITY_PATH = "/usr/bin/security";
/** `security find-generic-password` exit status for a missing item (errSecItemNotFound). */
export const SECURITY_ITEM_NOT_FOUND = 44;
/** Same rule as `pvault create-service-token --keychain`. */
export const KEYCHAIN_SERVICE_PATTERN = /^[A-Za-z0-9._][A-Za-z0-9._-]{0,63}$/;

export const LOOKUP_TIMEOUT_MS = 10_000;
export const LOOKUP_MAX_BYTES = 64 * 1024;

/**
 * Reads a generic password from the login Keychain. Errors are the raw
 * execFile errors (code 44 = not found, "ENOENT" = not macOS) so callers can
 * word them for their own setup.
 */
export async function findGenericPassword(service: string, account: string): Promise<string> {
  const { stdout } = await execFileAsync(
    SECURITY_PATH,
    ["find-generic-password", "-a", account, "-s", service, "-w"],
    { timeout: LOOKUP_TIMEOUT_MS, maxBuffer: LOOKUP_MAX_BYTES },
  );
  return stdout.trim();
}
