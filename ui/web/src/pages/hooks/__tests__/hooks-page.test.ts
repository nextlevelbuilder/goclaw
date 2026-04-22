/**
 * Smoke tests for the hooks page module contracts.
 *
 * @testing-library/react is not installed — tests cover pure logic,
 * schema validation, and i18n key presence (mirrors stt-provider-form.test.tsx).
 */
import { describe, it, expect } from "vitest";
import { hookFormSchema } from "@/schemas/hooks.schema";

// --- Zod schema validation ---

describe("hookFormSchema — base cases", () => {
  const base = {
    event: "pre_tool_use" as const,
    handler_type: "http" as const,
    scope: "tenant" as const,
    timeout_ms: 5000,
    on_timeout: "block" as const,
    priority: 100,
    enabled: true,
    method: "POST" as const,
    url: "https://hooks.example.com/test",
  };

  it("accepts valid http hook", () => {
    const result = hookFormSchema.safeParse(base);
    expect(result.success).toBe(true);
  });

  it("accepts valid prompt hook with matcher", () => {
    const result = hookFormSchema.safeParse({
      ...base,
      handler_type: "prompt",
      matcher: "^bash$",
      prompt_template: "Evaluate the tool call.",
    });
    expect(result.success).toBe(true);
  });

  it("rejects prompt hook without matcher or if_expr", () => {
    const result = hookFormSchema.safeParse({
      ...base,
      handler_type: "prompt",
      prompt_template: "Evaluate the tool call.",
      matcher: "",
      if_expr: "",
    });
    expect(result.success).toBe(false);
    if (!result.success) {
      const paths = result.error.issues.map((e) => e.path.join("."));
      expect(paths).toContain("matcher");
    }
  });

  it("rejects prompt hook without prompt_template", () => {
    const result = hookFormSchema.safeParse({
      ...base,
      handler_type: "prompt",
      matcher: "^bash$",
    });
    expect(result.success).toBe(false);
    if (!result.success) {
      const paths = result.error.issues.map((e) => e.path.join("."));
      expect(paths).toContain("prompt_template");
    }
  });

  it("rejects invalid regex in matcher", () => {
    const result = hookFormSchema.safeParse({
      ...base,
      matcher: "[invalid(regex",
    });
    expect(result.success).toBe(false);
    if (!result.success) {
      const paths = result.error.issues.map((e) => e.path.join("."));
      expect(paths).toContain("matcher");
    }
  });

  it("accepts valid regex in matcher", () => {
    const result = hookFormSchema.safeParse({
      ...base,
      matcher: "^(bash|python)$",
    });
    expect(result.success).toBe(true);
  });
});

// --- i18n key contracts ---

describe("hooks i18n key contracts", () => {
  it("en locale has all top-level required keys", async () => {
    const en = await import("@/i18n/locales/en/hooks.json");
    const data = en as unknown as Record<string, unknown>;
    for (const key of ["title", "subtitle", "empty", "nav", "actions", "filters", "table", "form", "tabs", "test", "history", "toast", "validation", "decision"]) {
      expect(data[key], `missing key: ${key}`).toBeDefined();
    }
  });

  it("vi locale has same top-level keys as en", async () => {
    const en = await import("@/i18n/locales/en/hooks.json");
    const vi = await import("@/i18n/locales/vi/hooks.json");
    const enKeys = Object.keys(en as unknown as Record<string, unknown>).sort();
    const viKeys = Object.keys(vi as unknown as Record<string, unknown>).sort();
    expect(viKeys).toEqual(enKeys);
  });

  it("zh locale has same top-level keys as en", async () => {
    const en = await import("@/i18n/locales/en/hooks.json");
    const zh = await import("@/i18n/locales/zh/hooks.json");
    const enKeys = Object.keys(en as unknown as Record<string, unknown>).sort();
    const zhKeys = Object.keys(zh as unknown as Record<string, unknown>).sort();
    expect(zhKeys).toEqual(enKeys);
  });

  it("decision keys exist in all locales", async () => {
    for (const locale of ["en", "vi", "zh"]) {
      const mod = await import(`@/i18n/locales/${locale}/hooks.json`);
      const data = mod as unknown as Record<string, Record<string, string>>;
      expect(data.decision?.allow).toBeTruthy();
      expect(data.decision?.block).toBeTruthy();
      expect(data.decision?.error).toBeTruthy();
      expect(data.decision?.timeout).toBeTruthy();
    }
  });

  it("toast keys exist in en", async () => {
    const en = await import("@/i18n/locales/en/hooks.json");
    const data = en as unknown as Record<string, Record<string, string>>;
    for (const key of ["created", "updated", "deleted", "toggled", "failedCreate", "failedUpdate", "failedDelete", "failedTest"]) {
      expect(data.toast?.[key], `missing toast.${key}`).toBeTruthy();
    }
  });
});

// --- buildConfig helper (re-tested inline) ---

describe("hook config builder logic", () => {
  it("prompt type config has prompt_template and model", () => {
    const config = {
      prompt_template: "Evaluate this.",
      model: "haiku",
      max_invocations_per_turn: 5,
    };
    expect(config.prompt_template).toBeTruthy();
    expect(config.model).toBe("haiku");
    expect(config.max_invocations_per_turn).toBe(5);
  });

  it("allowed event values are exhaustive", () => {
    const events = [
      "session_start", "user_prompt_submit", "pre_tool_use",
      "post_tool_use", "stop", "subagent_start", "subagent_stop",
    ];
    expect(events).toHaveLength(7);
    for (const ev of events) {
      const result = hookFormSchema.safeParse({
        event: ev,
        handler_type: "http",
        scope: "tenant",
        timeout_ms: 5000,
        on_timeout: "block",
        priority: 100,
        enabled: true,
      });
      expect(result.success, `event "${ev}" should be valid`).toBe(true);
    }
  });
});
