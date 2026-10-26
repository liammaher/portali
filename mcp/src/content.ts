import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";

/** Image types the model accepts inline; other images (HEIC, TIFF, SVG) are sent as a file. */
export const INLINE_IMAGES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

/**
 * The largest image sent inline: the model refuses one over 5 MB of base64,
 * and a refused image stays in the conversation and breaks every later turn.
 */
export const MAX_INLINE_IMAGE_BYTES = Math.floor((5 * 1024 * 1024 * 3) / 4);

/** Whether data starts as an image of type does, whatever a sender labelled it. */
function looksLike(type: string, data: Buffer): boolean {
  const starts = (sig: number[], at = 0) => sig.every((b, i) => data[at + i] === b);
  const ascii = (s: string, at = 0) => starts([...s].map((c) => c.charCodeAt(0)), at);
  switch (type) {
    case "image/png":
      return starts([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
    case "image/jpeg":
      return starts([0xff, 0xd8, 0xff]);
    case "image/gif":
      return ascii("GIF87a") || ascii("GIF89a");
    case "image/webp":
      return ascii("RIFF") && ascii("WEBP", 8);
    default:
      return false;
  }
}

/**
 * A file as tool output: an inline image when the model can take it (a type
 * it reads, bytes that are that type, small enough), else an embedded resource.
 */
export function fileContent(uri: string, mimeType: string, data: Buffer): CallToolResult {
  const blob = data.toString("base64");
  if (INLINE_IMAGES.has(mimeType) && data.length <= MAX_INLINE_IMAGE_BYTES && looksLike(mimeType, data)) {
    return { content: [{ type: "image", data: blob, mimeType }] };
  }
  return { content: [{ type: "resource", resource: { uri, mimeType, blob } }] };
}
