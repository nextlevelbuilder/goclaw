import { describe, expect, it } from "vitest";

describe("traces i18n filter keys", () => {
  it("all trace locales expose the same top-level keys", async () => {
    const en = await import("@/i18n/locales/en/traces.json");
    const vi = await import("@/i18n/locales/vi/traces.json");
    const zh = await import("@/i18n/locales/zh/traces.json");

    const enKeys = Object.keys(en as Record<string, unknown>).sort();
    expect(Object.keys(vi as Record<string, unknown>).sort()).toEqual(enKeys);
    expect(Object.keys(zh as Record<string, unknown>).sort()).toEqual(enKeys);
  });

  it("filter labels exist in all locales", async () => {
    for (const locale of ["en", "vi", "zh"]) {
      const mod = await import(`@/i18n/locales/${locale}/traces.json`);
      const data = mod as unknown as { filters?: Record<string, string> };
      for (const key of ["search", "status", "advanced", "from", "to", "toolName", "hasToolCalls", "clearAll"]) {
        expect(data.filters?.[key], `${locale} missing filters.${key}`).toBeTruthy();
      }
    }
  });
});
