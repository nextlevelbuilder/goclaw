/**
 * Unit tests for stt-provider-form logic.
 *
 * NOTE: @testing-library/react is not installed — tests cover pure logic
 * and module contracts rather than DOM rendering (mirrors voice-picker.test.tsx pattern).
 */
import { describe, it, expect } from "vitest";
import {
  validateSttProviders,
  buildSttPayload,
} from "../stt-provider-form";

// --- validateSttProviders ---

describe("validateSttProviders", () => {
  it("returns false for empty array", () => {
    expect(validateSttProviders([])).toBe(false);
  });

  it("returns true when one provider selected", () => {
    expect(validateSttProviders(["elevenlabs"])).toBe(true);
  });

  it("returns true when multiple providers selected", () => {
    expect(validateSttProviders(["elevenlabs", "proxy"])).toBe(true);
  });
});

// --- buildSttPayload ---

describe("buildSttPayload — default shape", () => {
  const base = {
    providers: ["elevenlabs", "proxy"] as ("elevenlabs" | "proxy")[],
    elevenlabs: { api_key: "", default_language: "en" },
    proxy: { url: "", api_key: "", tenant_id: "" },
    whatsapp_enabled: false,
  };

  it("includes all required top-level keys", () => {
    const payload = buildSttPayload(base);
    expect(payload).toHaveProperty("providers");
    expect(payload).toHaveProperty("elevenlabs");
    expect(payload).toHaveProperty("proxy");
    expect(payload).toHaveProperty("whatsapp_enabled");
  });

  it("providers array matches input", () => {
    const payload = buildSttPayload(base);
    expect(payload.providers).toEqual(["elevenlabs", "proxy"]);
  });

  it("elevenlabs sub-object has api_key and default_language", () => {
    const payload = buildSttPayload({
      ...base,
      elevenlabs: { api_key: "xi-abc", default_language: "vi" },
    });
    const el = payload.elevenlabs as Record<string, unknown>;
    expect(el.api_key).toBe("xi-abc");
    expect(el.default_language).toBe("vi");
  });

  it("proxy sub-object has url, api_key, tenant_id", () => {
    const payload = buildSttPayload({
      ...base,
      proxy: { url: "https://proxy.example.com", api_key: "secret", tenant_id: "acme" },
    });
    const px = payload.proxy as Record<string, unknown>;
    expect(px.url).toBe("https://proxy.example.com");
    expect(px.api_key).toBe("secret");
    expect(px.tenant_id).toBe("acme");
  });

  it("whatsapp_enabled defaults false", () => {
    const payload = buildSttPayload(base);
    expect(payload.whatsapp_enabled).toBe(false);
  });

  it("whatsapp_enabled reflects true when set", () => {
    const payload = buildSttPayload({ ...base, whatsapp_enabled: true });
    expect(payload.whatsapp_enabled).toBe(true);
  });
});

// --- privacy banner: verify the i18n key constant is referenced ---

describe("sttForm i18n key contract", () => {
  it("whatsappPrivacyWarning key resolves to a non-empty string in en locale", async () => {
    const en = await import("@/i18n/locales/en/tools.json");
    const form = (en as unknown as Record<string, Record<string, Record<string, string>>>)
      .builtin?.sttForm;
    expect(form).toBeDefined();
    expect(form!.whatsappPrivacyWarning).toBeTruthy();
    expect(form!.whatsappPrivacyWarning).toContain("WhatsApp");
  });

  it("vi locale has whatsappPrivacyWarning", async () => {
    const vi = await import("@/i18n/locales/vi/tools.json");
    const form = (vi as unknown as Record<string, Record<string, Record<string, string>>>)
      .builtin?.sttForm;
    expect(form?.whatsappPrivacyWarning).toBeTruthy();
  });

  it("zh locale has whatsappPrivacyWarning", async () => {
    const zh = await import("@/i18n/locales/zh/tools.json");
    const form = (zh as unknown as Record<string, Record<string, Record<string, string>>>)
      .builtin?.sttForm;
    expect(form?.whatsappPrivacyWarning).toBeTruthy();
  });

  it("providersRequiredError key exists in all locales", async () => {
    const en = await import("@/i18n/locales/en/tools.json");
    const vi = await import("@/i18n/locales/vi/tools.json");
    const zh = await import("@/i18n/locales/zh/tools.json");
    for (const locale of [en, vi, zh]) {
      const form = (locale as unknown as Record<string, Record<string, Record<string, string>>>)
        .builtin?.sttForm;
      expect(form?.providersRequiredError).toBeTruthy();
    }
  });
});
