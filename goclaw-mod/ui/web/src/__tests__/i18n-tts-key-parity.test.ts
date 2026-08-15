/**
 * i18n TTS key parity tests.
 *
 * Ensures every leaf key present in the English (EN) tts.json exists in the
 * Vietnamese (VI) and Simplified Chinese (ZH) translations. Prevents silent
 * fallback to English in production caused by missing keys.
 *
 * Also covers desktop locale files — they have a distinct keyset from web but
 * must themselves be internally consistent across EN/VI/ZH.
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve, dirname } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type NestedRecord = { [key: string]: string | NestedRecord };

function loadLocale(relPath: string): NestedRecord {
  const abs = resolve(__dirname, relPath);
  return JSON.parse(readFileSync(abs, "utf-8")) as NestedRecord;
}

/**
 * Recursively collect all leaf key paths from a nested object.
 * Returns dot-separated paths, e.g. "openai.speed.label".
 */
function collectLeafPaths(obj: NestedRecord, prefix = ""): string[] {
  const paths: string[] = [];
  for (const [key, value] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "string") {
      paths.push(path);
    } else if (typeof value === "object" && value !== null) {
      paths.push(...collectLeafPaths(value as NestedRecord, path));
    }
  }
  return paths;
}

/**
 * Resolves a dot-separated path in a nested object.
 */
function getByPath(obj: NestedRecord, path: string): unknown {
  return path.split(".").reduce<unknown>((acc, segment) => {
    if (acc !== null && typeof acc === "object") {
      return (acc as NestedRecord)[segment];
    }
    return undefined;
  }, obj);
}

/**
 * Asserts every leaf key from `source` exists (as a string) in `target`.
 * Reports all missing keys at once.
 */
function assertKeyParity(
  source: NestedRecord,
  target: NestedRecord,
  targetLabel: string,
): void {
  const leafPaths = collectLeafPaths(source);
  const missing: string[] = [];

  for (const path of leafPaths) {
    const value = getByPath(target, path);
    if (typeof value !== "string") {
      missing.push(path);
    }
  }

  expect(missing, `${targetLabel} is missing keys`).toEqual([]);
}

// ---------------------------------------------------------------------------
// Locale paths — relative to this test file (src/__tests__/)
// ---------------------------------------------------------------------------

const WEB_LOCALES = "../i18n/locales";
// ui/desktop/frontend: from ui/web/src/__tests__ go up 3 dirs to ui/, then into desktop/frontend
const DESKTOP_LOCALES = "../../../desktop/frontend/src/i18n/locales";

// ---------------------------------------------------------------------------
// Web locale parity
// ---------------------------------------------------------------------------

describe("Web TTS i18n parity", () => {
  const webEN = loadLocale(`${WEB_LOCALES}/en/tts.json`);
  const webVI = loadLocale(`${WEB_LOCALES}/vi/tts.json`);
  const webZH = loadLocale(`${WEB_LOCALES}/zh/tts.json`);

  it("VI contains every key present in EN", () => {
    assertKeyParity(webEN, webVI, "web vi/tts.json");
  });

  it("ZH contains every key present in EN", () => {
    assertKeyParity(webEN, webZH, "web zh/tts.json");
  });
});

// ---------------------------------------------------------------------------
// Desktop locale parity
// ---------------------------------------------------------------------------

describe("Desktop TTS i18n parity", () => {
  const desktopEN = loadLocale(`${DESKTOP_LOCALES}/en/tts.json`);
  const desktopVI = loadLocale(`${DESKTOP_LOCALES}/vi/tts.json`);
  const desktopZH = loadLocale(`${DESKTOP_LOCALES}/zh/tts.json`);

  it("VI contains every key present in EN", () => {
    assertKeyParity(desktopEN, desktopVI, "desktop vi/tts.json");
  });

  it("ZH contains every key present in EN", () => {
    assertKeyParity(desktopEN, desktopZH, "desktop zh/tts.json");
  });
});
