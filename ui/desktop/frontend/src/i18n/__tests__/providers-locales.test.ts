/**
 * providers.json locale integrity for the desktop UI.
 *
 * JSON.parse silently keeps the last of two identical keys, so a duplicated
 * top-level block hides every string of the first one. This test fails on any
 * duplicate key and checks that all locales share the English key set, ignoring
 * plural suffixes (languages legitimately differ in their plural categories).
 */
import { describe, it, expect } from "vitest";

const RAW = import.meta.glob("../locales/*/providers.json", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;
const LOCALES = ["en", "ru", "vi", "zh"] as const;
const PLURAL_SUFFIX = /_(zero|one|two|few|many|other)$/;

type NestedRecord = { [key: string]: string | NestedRecord };

function readLocale(locale: string): string {
  return RAW[`../locales/${locale}/providers.json`];
}

/** Returns the dotted path of every object key that appears twice in one object. */
function findDuplicateKeys(json: string): string[] {
  const duplicates: string[] = [];
  const stack: { keys: Set<string>; path: string }[] = [];
  let pendingKey: string | null = null;
  let i = 0;
  while (i < json.length) {
    const ch = json[i];
    if (ch === '"') {
      let j = i + 1;
      while (json[j] !== '"') j += json[j] === "\\" ? 2 : 1;
      const text = JSON.parse(json.slice(i, j + 1)) as string;
      let k = j + 1;
      while (/\s/.test(json[k] ?? "")) k++;
      const top = stack[stack.length - 1];
      if (json[k] === ":" && top) {
        if (top.keys.has(text)) duplicates.push(top.path ? `${top.path}.${text}` : text);
        top.keys.add(text);
        pendingKey = text;
      }
      i = j + 1;
      continue;
    }
    if (ch === "{") {
      const parent = stack[stack.length - 1];
      const path = parent && pendingKey ? (parent.path ? `${parent.path}.${pendingKey}` : pendingKey) : "";
      stack.push({ keys: new Set(), path });
      pendingKey = null;
    } else if (ch === "}") {
      stack.pop();
    }
    i++;
  }
  return duplicates;
}

function flatten(obj: NestedRecord, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return typeof value === "string" ? [path] : flatten(value, path);
  });
}

const baseKeys = (locale: string) =>
  new Set(flatten(JSON.parse(readLocale(locale)) as NestedRecord).map((k) => k.replace(PLURAL_SUFFIX, "")));

describe("desktop providers.json", () => {
  for (const locale of LOCALES) {
    it(`${locale} has no duplicate keys`, () => {
      expect(findDuplicateKeys(readLocale(locale))).toEqual([]);
    });
  }

  const en = baseKeys("en");
  for (const locale of LOCALES.filter((l) => l !== "en")) {
    it(`${locale} has the same keys as en (ignoring plural suffixes)`, () => {
      const keys = baseKeys(locale);
      const missing = [...en].filter((k) => !keys.has(k)).sort();
      const extra = [...keys].filter((k) => !en.has(k)).sort();
      expect({ missing, extra }).toEqual({ missing: [], extra: [] });
    });
  }
});
