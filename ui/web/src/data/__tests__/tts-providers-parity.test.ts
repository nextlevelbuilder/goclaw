/**
 * Parity test: web catalog vs desktop catalog provider IDs must stay in sync.
 *
 * Uses fs.readFileSync + regex extraction instead of cross-package imports to
 * avoid build-system coupling. Path fragility acknowledged — if the directory
 * layout changes, update the paths below.
 *
 * Web catalog:     ui/web/src/data/tts-providers.ts
 * Desktop catalog: ui/desktop/frontend/src/data/tts-providers.ts
 */
import { describe, it, expect } from "vitest";
import fs from "fs";
import path from "path";

/**
 * Extracts top-level provider keys from a TTS_PROVIDERS object literal.
 * Matches lines like `  openai: {` or `  elevenlabs: {` inside the TTS_PROVIDERS block.
 */
function extractProviderIds(filePath: string): string[] {
  const src = fs.readFileSync(filePath, "utf8");
  // Find the TTS_PROVIDERS object block, then extract top-level keys
  const providerBlockMatch = src.match(/TTS_PROVIDERS[^=]*=\s*\{([\s\S]*?)^};/m);
  if (!providerBlockMatch || !providerBlockMatch[1]) {
    // Fallback: extract all top-level identifier keys followed by ': {'
    const matches = Array.from(src.matchAll(/^\s{2}(\w+):\s*\{/gm));
    return matches.map((m) => m[1]).filter((id): id is string => id !== undefined);
  }
  const block = providerBlockMatch[1];
  const matches = Array.from(block.matchAll(/^\s{2}(\w+):\s*\{/gm));
  return matches.map((m) => m[1]).filter((id): id is string => id !== undefined);
}

describe("TTS provider catalog parity (web ↔ desktop)", () => {
  const webCatalog = path.resolve(__dirname, "../tts-providers.ts");
  const desktopCatalog = path.resolve(
    __dirname,
    "../../../../../ui/desktop/frontend/src/data/tts-providers.ts",
  );

  it("both catalog files exist on disk", () => {
    expect(fs.existsSync(webCatalog), `web catalog missing: ${webCatalog}`).toBe(true);
    expect(fs.existsSync(desktopCatalog), `desktop catalog missing: ${desktopCatalog}`).toBe(true);
  });

  it("web and desktop catalogs expose identical provider ids", () => {
    const idsWeb = extractProviderIds(webCatalog).sort();
    const idsDesktop = extractProviderIds(desktopCatalog).sort();

    expect(idsWeb.length).toBeGreaterThan(0);
    expect(idsDesktop.length).toBeGreaterThan(0);
    expect(idsWeb).toEqual(idsDesktop);
  });

  it("both catalogs include the 4 expected providers", () => {
    const idsWeb = extractProviderIds(webCatalog).sort();
    expect(idsWeb).toEqual(["edge", "elevenlabs", "minimax", "openai"]);
  });
});
