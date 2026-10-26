import { test } from "node:test";
import assert from "node:assert/strict";
import { fileContent, MAX_INLINE_IMAGE_BYTES } from "./content.js";

const PNG = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13]);
const JPEG = Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 16]);
const kind = (type: string, data: Buffer) => fileContent("vault://documents/x", type, data).content[0].type;

// HEIC (iPhone scans) and TIFF are images the model can't take inline, so
// they go out as a file like a PDF does.
test("fileContent sends only model-readable images inline", () => {
  assert.equal(kind("image/png", PNG), "image");
  assert.equal(kind("image/jpeg", JPEG), "image");
  assert.equal(kind("image/gif", Buffer.from("GIF89a....")), "image");
  assert.equal(kind("image/webp", Buffer.from("RIFF\0\0\0\0WEBPVP8 ")), "image");
  for (const type of ["image/heic", "image/tiff", "image/svg+xml", "application/pdf"]) {
    assert.equal(kind(type, PNG), "resource", type);
  }
});

// An image the model refuses stays in the conversation and breaks every later
// turn: one labelled as an image that isn't (a PEC sender chooses the label),
// or over the model's size limit, goes out as a file instead.
test("fileContent sends mislabelled or oversized images as a file", () => {
  assert.equal(kind("image/png", Buffer.from("%PDF-1.7 not a png")), "resource");
  assert.equal(kind("image/jpeg", PNG), "resource");
  assert.equal(kind("image/webp", Buffer.from("RIFF\0\0\0\0AVI LIST")), "resource");
  const big = Buffer.alloc(MAX_INLINE_IMAGE_BYTES + 1);
  JPEG.copy(big);
  assert.equal(kind("image/jpeg", big), "resource");
  assert.equal(kind("image/jpeg", big.subarray(0, MAX_INLINE_IMAGE_BYTES)), "image");
});
