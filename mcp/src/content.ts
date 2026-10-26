import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";

/** Image types the model accepts inline; other images (HEIC, TIFF, SVG) are sent as a file. */
export const INLINE_IMAGES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

/** A file as tool output: an inline image when the model can take it, else an embedded resource. */
export function fileContent(uri: string, mimeType: string, data: Buffer): CallToolResult {
  const blob = data.toString("base64");
  if (INLINE_IMAGES.has(mimeType)) {
    return { content: [{ type: "image", data: blob, mimeType }] };
  }
  return { content: [{ type: "resource", resource: { uri, mimeType, blob } }] };
}
