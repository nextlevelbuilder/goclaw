import i18next from "i18next";
import { describe, expect, it } from "vitest";
import { PROVIDER_TYPES, providerTypeLabel } from "./providers";
import enProviders from "../i18n/locales/en/providers.json";
import viProviders from "../i18n/locales/vi/providers.json";
import zhProviders from "../i18n/locales/zh/providers.json";
import koProviders from "../i18n/locales/ko/providers.json";
import ruProviders from "../i18n/locales/ru/providers.json";

const locales = { en: enProviders, vi: viProviders, zh: zhProviders, ko: koProviders, ru: ruProviders };

async function makeI18n() {
  const instance = i18next.createInstance();
  await instance.init({
    lng: "en",
    fallbackLng: "en",
    defaultNS: "providers",
    ns: ["providers"],
    resources: Object.fromEntries(Object.entries(locales).map(([lng, res]) => [lng, { providers: res }])),
    interpolation: { escapeValue: false },
  });
  return instance;
}

const custom = PROVIDER_TYPES.find((pt) => pt.value === "custom");

describe("providerTypeLabel", () => {
  it("has a translated label key for the custom alias", () => {
    expect(custom?.labelKey).toBe("providerTypes.custom");
  });

  it("defines the custom label in every locale", () => {
    for (const [lng, res] of Object.entries(locales)) {
      const label = (res as { providerTypes?: { custom?: string } }).providerTypes?.custom;
      expect(label, lng).toBeTruthy();
    }
  });

  it("follows locale switches", async () => {
    const i18n = await makeI18n();
    const translate = (key: string) => i18n.t(key);
    expect(providerTypeLabel(custom!, translate)).toBe(enProviders.providerTypes.custom);
    await i18n.changeLanguage("vi");
    expect(providerTypeLabel(custom!, translate)).toBe(viProviders.providerTypes.custom);
    await i18n.changeLanguage("zh");
    expect(providerTypeLabel(custom!, translate)).toBe(zhProviders.providerTypes.custom);
    await i18n.changeLanguage("en");
    expect(providerTypeLabel(custom!, translate)).toBe(enProviders.providerTypes.custom);
  });

  it("keeps static vendor names untranslated", async () => {
    const i18n = await makeI18n();
    await i18n.changeLanguage("zh");
    const compat = PROVIDER_TYPES.find((pt) => pt.value === "openai_compat")!;
    expect(providerTypeLabel(compat, (key) => i18n.t(key))).toBe(compat.label);
  });
});
