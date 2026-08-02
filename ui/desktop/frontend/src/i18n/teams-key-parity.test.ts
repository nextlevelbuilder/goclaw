import { describe, expect, it } from 'vitest'
import en from './locales/en/teams.json'
import ko from './locales/ko/teams.json'
import ru from './locales/ru/teams.json'
import vi from './locales/vi/teams.json'
import zh from './locales/zh/teams.json'

type NestedRecord = { [key: string]: string | NestedRecord }

function leafPaths(value: NestedRecord, prefix = ''): string[] {
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return typeof child === 'string' ? [path] : leafPaths(child, path)
  }).sort()
}

const locales = { vi, zh, ko, ru }

describe('Desktop teams i18n key parity', () => {
  for (const language of ['vi', 'zh', 'ko', 'ru'] as const) {
    it(`${language}/teams.json exactly matches the English key set`, () => {
      expect(leafPaths(locales[language])).toEqual(leafPaths(en))
    })
  }
})
