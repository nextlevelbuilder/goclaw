import { describe, expect, it } from "vitest";
import webEn from "../i18n/locales/en/teams.json";
import webKo from "../i18n/locales/ko/teams.json";
import webRu from "../i18n/locales/ru/teams.json";
import webVi from "../i18n/locales/vi/teams.json";
import webZh from "../i18n/locales/zh/teams.json";
import desktopEn from "../../../desktop/frontend/src/i18n/locales/en/teams.json";
import desktopKo from "../../../desktop/frontend/src/i18n/locales/ko/teams.json";
import desktopRu from "../../../desktop/frontend/src/i18n/locales/ru/teams.json";
import desktopVi from "../../../desktop/frontend/src/i18n/locales/vi/teams.json";
import desktopZh from "../../../desktop/frontend/src/i18n/locales/zh/teams.json";

type NestedRecord = { [key: string]: string | NestedRecord };

function leafPaths(value: NestedRecord, prefix = ""): string[] {
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return typeof child === "string" ? [path] : leafPaths(child, path);
  }).sort();
}

function expectExactKeyParity(source: NestedRecord, target: NestedRecord, label: string) {
  expect(leafPaths(target), `${label} key set differs from English`).toEqual(leafPaths(source));
}

const webLocales = { vi: webVi, zh: webZh, ko: webKo, ru: webRu };
const desktopLocales = { vi: desktopVi, zh: desktopZh, ko: desktopKo, ru: desktopRu };

describe("teams i18n key parity", () => {
  for (const language of ["vi", "zh", "ko", "ru"] as const) {
    it(`Web ${language} exactly matches the English key set`, () => {
      expectExactKeyParity(webEn, webLocales[language], `Web ${language}/teams.json`);
    });

    it(`Desktop ${language} exactly matches the English key set`, () => {
      expectExactKeyParity(desktopEn, desktopLocales[language], `Desktop ${language}/teams.json`);
    });
  }
});
