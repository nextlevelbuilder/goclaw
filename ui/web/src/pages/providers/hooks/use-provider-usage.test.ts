import { describe, expect, it } from "vitest";
import fixture from "./__fixtures__/provider-usage-detail.json";
import en from "@/i18n/locales/en/providers.json";
import ko from "@/i18n/locales/ko/providers.json";
import ru from "@/i18n/locales/ru/providers.json";
import vi from "@/i18n/locales/vi/providers.json";
import zh from "@/i18n/locales/zh/providers.json";
import {
  KNOWN_ERROR_CLASSES,
  errorClassKey,
  errorRate,
  formatErrorRate,
  formatLatency,
  parseProviderUsageDetail,
  parseUsageRange,
  rangeDays,
  rangeWindow,
} from "./use-provider-usage";

describe("parseProviderUsageDetail", () => {
  it("parses the real handler fixture, keeping newest-first order", () => {
    const d = parseProviderUsageDetail(fixture);
    expect(d.provider).toBe("dashscope");
    expect(d.calls).toBe(1284);
    expect(d.errors).toBe(80);
    expect(d.latency_ms).toEqual({ p50: 1400, p95: 4800 });
    expect(d.recent_errors.map((e) => e.error_class)).toEqual(["rate_limit", "timeout", "auth"]);
    expect(d.recent_errors[0]).toEqual({ at: "2026-11-06T04:38:10Z", error_class: "rate_limit", count: 2 });
  });

  it("drops malformed rows and defaults missing fields", () => {
    const d = parseProviderUsageDetail({
      calls: "12",
      recent_errors: [{ at: "bad" }, { at: "2026-11-06T00:00:00Z", count: 3 }, "x"],
    });
    expect(d.calls).toBe(0);
    expect(d.errors).toBe(0);
    expect(d.latency_ms).toEqual({ p50: 0, p95: 0 });
    expect(d.recent_errors).toEqual([{ at: "2026-11-06T00:00:00Z", error_class: "unknown", count: 3 }]);
  });

  it("rejects non-object payloads", () => {
    expect(() => parseProviderUsageDetail(undefined)).toThrow();
  });
});

describe("error rate", () => {
  it("divides errors by calls from the fixture", () => {
    const d = parseProviderUsageDetail(fixture);
    expect(formatErrorRate(errorRate(d.errors, d.calls))).toBe("6.2%");
  });

  it("handles zero calls and tiny rates", () => {
    expect(errorRate(0, 0)).toBeNull();
    expect(formatErrorRate(null)).toBe("—");
    expect(formatErrorRate(errorRate(0, 50))).toBe("0.0%");
    expect(formatErrorRate(errorRate(1, 5000))).toBe("<0.1%");
    expect(formatErrorRate(errorRate(10, 5))).toBe("100.0%");
  });
});

describe("formatLatency", () => {
  it("formats p50/p95 from the fixture in seconds", () => {
    const d = parseProviderUsageDetail(fixture);
    expect(formatLatency(d.latency_ms.p50)).toBe("1.4 s");
    expect(formatLatency(d.latency_ms.p95)).toBe("4.8 s");
  });

  it("keeps sub-second values in ms and blanks missing values", () => {
    expect(formatLatency(349.6)).toBe("350 ms");
    expect(formatLatency(0)).toBe("—");
    expect(formatLatency(null)).toBe("—");
    expect(formatLatency(Number.NaN)).toBe("—");
  });
});

describe("ranges", () => {
  it("parses the URL value and falls back to 7d", () => {
    expect(parseUsageRange("24h")).toBe("24h");
    expect(parseUsageRange("30d")).toBe("30d");
    expect(parseUsageRange("1y")).toBe("7d");
    expect(parseUsageRange(null)).toBe("7d");
  });

  it("builds an RFC3339 window ending now", () => {
    const now = Date.parse("2026-11-06T12:00:00Z");
    expect(rangeWindow("24h", now)).toEqual({ from: "2026-11-05T12:00:00.000Z", to: "2026-11-06T12:00:00.000Z" });
    expect(rangeWindow("30d", now).from).toBe("2026-10-07T12:00:00.000Z");
    expect(rangeDays("7d")).toBe(7);
  });
});

describe("error class labels", () => {
  const locales = { en, ko, ru, vi, zh } as Record<string, Record<string, unknown>>;

  function lookup(obj: Record<string, unknown>, key: string): unknown {
    return key.split(".").reduce<unknown>((acc, part) => (acc as Record<string, unknown> | undefined)?.[part], obj);
  }

  it("maps known classes to their own key and unknown ones to the fallback", () => {
    expect(errorClassKey("rate_limit")).toBe("usage.errorClass.rate_limit");
    expect(errorClassKey("auth_permanent")).toBe("usage.errorClass.auth_permanent");
    expect(errorClassKey("brand_new_class")).toBe("usage.errorClass.unknown");
    expect(errorClassKey("")).toBe("usage.errorClass.unknown");
    expect(errorClassKey(undefined)).toBe("usage.errorClass.unknown");
  });

  it("has a translated label for every class in every locale", () => {
    for (const [lang, dict] of Object.entries(locales)) {
      for (const cls of KNOWN_ERROR_CLASSES) {
        const label = lookup(dict, errorClassKey(cls));
        expect(typeof label, `${lang}: ${cls}`).toBe("string");
        expect((label as string).length, `${lang}: ${cls}`).toBeGreaterThan(0);
      }
    }
  });

  it("keeps the Usage tab key sets identical across locales", () => {
    const flatten = (obj: Record<string, unknown>, prefix = ""): string[] =>
      Object.entries(obj).flatMap(([k, v]) =>
        typeof v === "object" && v !== null ? flatten(v as Record<string, unknown>, `${prefix}${k}.`) : [`${prefix}${k}`],
      );
    // Scoped to the Usage tab subtrees; other providers.json sections have older, independent gaps.
    const usageKeys = (dict: Record<string, unknown>) =>
      flatten(dict).filter((k) => k.startsWith("usage.") || k.startsWith("detail.tabs.")).sort();
    const enKeys = usageKeys(locales.en!);
    expect(enKeys.length).toBeGreaterThan(0);
    for (const lang of ["ko", "ru", "vi", "zh"]) {
      expect(usageKeys(locales[lang]!), lang).toEqual(enKeys);
    }
  });
});
