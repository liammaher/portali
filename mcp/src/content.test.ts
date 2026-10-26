import { test } from "node:test";
import assert from "node:assert/strict";
import { fileContent } from "./content.js";

// HEIC (iPhone scans) and TIFF are images the model can't take inline, so
// they go out as a file like a PDF does.
test("fileContent sends only model-readable images inline", () => {
  const data = Buffer.from("img");
  assert.equal(fileContent("vault://documents/x", "image/png", data).content[0].type, "image");
  for (const type of ["image/heic", "image/tiff", "image/svg+xml", "application/pdf"]) {
    const [item] = fileContent("vault://documents/x", type, data).content;
    assert.equal(item.type, "resource", type);
  }
});
