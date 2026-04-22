/**
 * Parity test: web catalog vs desktop catalog — reduced shape (Phase C).
 *
 * Asserts that both catalogs expose identical provider IDs and that
 * TtsProviderMeta fields (id, title, color, desc) are present in both.
 * Uses fs.readFileSync + regex to avoid cross-package build coupling.
 */
import { describe, it, expect } from "vitest";
import fs from "fs";
import path from "path";

function extractProviderIds(filePath: string): string[] {
  const src = fs.readFileSync(filePath, "utf8");
  // Match top-level keys inside TTS_PROVIDERS = { ... }
  const blockMatch = src.match(/TTS_PROVIDERS[^=]*=\s*\{([\s\S]*?)^};/m);
  if (!blockMatch?.[1]) {
    const matches = Array.from(src.matchAll(/^\s{2}(\w+):\s*\{/gm));
    return matches.map((m) => m[1]).filter((id): id is string => !!id);
  }
  const block = blockMatch[1];
  const matches = Array.from(block.matchAll(/^\s{2}(\w+):\s*\{/gm));
  return matches.map((m) => m[1]).filter((id): id is string => !!id);
}

function hasReducedShape(filePath: string): boolean {
  const src = fs.readFileSync(filePath, "utf8");
  // Verify removed fields are absent at the type/interface level
  const hasModels = /^\s+models\s*:/m.test(src);
  const hasVoices = /^\s+voices\s*:/m.test(src);
  const hasDynamic = /^\s+dynamic\s*:/m.test(src);
  // Verify kept fields are present (id, title, color, desc per Phase C spec)
  const hasId = /id\s*:/m.test(src);
  const hasTitle = /title\s*:/m.test(src);
  const hasColor = /color\s*:/m.test(src);
  const hasDesc = /desc\s*:/m.test(src);
  return !hasModels && !hasVoices && !hasDynamic && hasId && hasTitle && hasColor && hasDesc;
}

describe("TTS provider catalog parity (web ↔ desktop) — reduced shape", () => {
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

  it("both catalogs include the 5 expected providers", () => {
    const idsWeb = extractProviderIds(webCatalog).sort();
    expect(idsWeb).toEqual(["edge", "elevenlabs", "gemini", "minimax", "openai"]);
  });

  it("both catalogs use reduced shape (no models/voices/dynamic fields)", () => {
    expect(hasReducedShape(webCatalog), "web catalog has unexpected fields").toBe(true);
    expect(hasReducedShape(desktopCatalog), "desktop catalog has unexpected fields").toBe(true);
  });
});
