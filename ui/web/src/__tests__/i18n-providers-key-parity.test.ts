/**
 * providers.json locale parity.
 *
 * Every web locale must define exactly the same flattened key set as English.
 * A missing key renders its raw path in the UI; an extra key is dead text that
 * hides drift between locales.
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve, dirname } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const LOCALES_DIR = resolve(__dirname, "../i18n/locales");
const LOCALES = ["ko", "ru", "vi", "zh"] as const;

type NestedRecord = { [key: string]: string | NestedRecord };

function loadProviders(locale: string): NestedRecord {
  const raw = readFileSync(resolve(LOCALES_DIR, locale, "providers.json"), "utf-8");
  return JSON.parse(raw) as NestedRecord;
}

function flatten(obj: NestedRecord, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return typeof value === "string" ? [path] : flatten(value, path);
  });
}

describe("web providers.json key parity", () => {
  const enKeys = new Set(flatten(loadProviders("en")));

  for (const locale of LOCALES) {
    it(`${locale} has the same keys as en`, () => {
      const keys = new Set(flatten(loadProviders(locale)));
      const missing = [...enKeys].filter((k) => !keys.has(k)).sort();
      const extra = [...keys].filter((k) => !enKeys.has(k)).sort();
      expect({ missing, extra }).toEqual({ missing: [], extra: [] });
    });
  }
});
