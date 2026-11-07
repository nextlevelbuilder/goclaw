import { describe, expect, it } from 'vitest'

/**
 * Every namespace must expose the same keys in en, vi, zh and ru.
 *
 * Plural forms are compared by their base key: i18next picks `_one`, `_few`,
 * `_many`, `_other` per language, so `count_one` in English and only
 * `count_other` in Vietnamese are equivalent.
 */

const LOCALES = ['en', 'vi', 'zh', 'ru'] as const
type Locale = (typeof LOCALES)[number]

/**
 * Known gaps in namespaces that are rebuilt later. Each entry is
 * `<namespace>:<locale>:<key>`. Remove an entry once the key is translated;
 * the test fails on stale entries so this list can only shrink.
 */
const KNOWN_GAPS = new Set<string>([])

const files = import.meta.glob<Record<string, unknown>>('../locales/*/*.json', {
  eager: true,
  import: 'default',
})

const PLURAL_SUFFIX = /_(zero|one|two|few|many|other)$/

function flatten(obj: Record<string, unknown>, prefix = ''): string[] {
  return Object.entries(obj).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      return flatten(value as Record<string, unknown>, path)
    }
    return [path.replace(PLURAL_SUFFIX, '')]
  })
}

function load(): Map<string, Map<Locale, Set<string>>> {
  const byNamespace = new Map<string, Map<Locale, Set<string>>>()
  for (const [file, json] of Object.entries(files)) {
    const match = /locales\/([^/]+)\/([^/]+)\.json$/.exec(file)
    if (!match) continue
    const [, locale, ns] = match
    if (!(LOCALES as readonly string[]).includes(locale)) continue
    const perLocale = byNamespace.get(ns) ?? new Map<Locale, Set<string>>()
    perLocale.set(locale as Locale, new Set(flatten(json)))
    byNamespace.set(ns, perLocale)
  }
  return byNamespace
}

const namespaces = load()

describe('locale parity', () => {
  it('finds the locale files', () => {
    expect(namespaces.size).toBeGreaterThan(10)
    expect(namespaces.has('desktop')).toBe(true)
  })

  for (const [ns, perLocale] of namespaces) {
    it(`${ns}: every locale has the file`, () => {
      expect([...perLocale.keys()].sort()).toEqual([...LOCALES].sort())
    })

    it(`${ns}: every locale has the same keys`, () => {
      const all = new Set<string>()
      for (const keys of perLocale.values()) keys.forEach((k) => all.add(k))

      const problems: string[] = []
      for (const locale of LOCALES) {
        const keys = perLocale.get(locale) ?? new Set<string>()
        for (const key of all) {
          if (!keys.has(key) && !KNOWN_GAPS.has(`${ns}:${locale}:${key}`)) {
            problems.push(`${locale} is missing ${key}`)
          }
        }
      }
      expect(problems).toEqual([])
    })
  }

  it('the known-gap list has no stale entries', () => {
    const stale = [...KNOWN_GAPS].filter((entry) => {
      const [ns, locale, key] = entry.split(':')
      const keys = namespaces.get(ns)?.get(locale as Locale)
      return !keys || keys.has(key)
    })
    expect(stale).toEqual([])
  })
})
