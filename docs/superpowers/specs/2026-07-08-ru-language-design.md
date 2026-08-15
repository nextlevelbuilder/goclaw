# Design: Russian language (`ru`) support for GoClaw

**Date:** 2026-07-08
**Branch:** `feature/ru-lang`
**Status:** Approved

## Goal

Add Russian (`ru`) as a fully supported UI/message language across all three product
surfaces (backend i18n catalog, Web UI, Desktop UI), at **full parity with English** —
not the partial coverage the Korean (`ko`) locale currently ships.

## Reference precedent

`ko` (Korean) was the most recently added language. It is the integration template for
`ru` on the **backend** and **Web UI**. The **Desktop UI** has no `ko` at all, so `ru`
there is net-new work patterned on the existing `zh` wiring.

**Key deviation from `ko`:** `ko` is intentionally partial (backend catalog = 131 of 281
keys; Web UI = 38 of 41 namespaces, omitting `hooks`/`webhooks`/`workstations`). Because
the user chose **full parity with English**, `ru` covers **all 281 backend keys** and
**all 41 Web namespaces**.

## Scope by surface

### 1. Backend (Go) — full catalog (~281 keys)

| File | Change |
|------|--------|
| `internal/i18n/i18n.go` | Add `LocaleRU = "ru"` const; add `LocaleRU` to `IsSupported()` switch. `Normalize()` handles `ru`/`ru-RU` prefix automatically once `IsSupported` knows it. |
| `internal/i18n/catalog_ru.go` (new) | `func init() { register(LocaleRU, map[string]string{...}) }` — **all 281 keys** from `catalog_en.go`. |
| `internal/systemmessages/resolver.go` | Add `i18n.LocaleRU:` entry in each of the **10 maps** (5 messages × Labels + Descriptions), lines ~51,57,73,79,95,101,118,124,136,142. |
| `internal/gateway/methods/config.go:366` | Add `"ru"` to the `default_locale` enum `[]string{"en","vi","zh","ko"}`. |
| `internal/i18n/git_keys_parity_test.go:34` | **Add** `LocaleRU` to the locale list. Correct for a full catalog (ko is excluded only because it is partial). |
| `internal/config/config.go:128` | Update doc-comment to mention `ru` (informational, no logic). |

Ingest paths need **no per-locale edit** (verified): `internal/gateway/router.go`,
`internal/http/auth.go` `extractLocale`, `internal/store/context.go` `WithLocale` — all
run the locale through `i18n.Normalize()` and work for `ru` automatically.

### 2. Web UI — full 41 namespaces

| File | Change |
|------|--------|
| `ui/web/src/i18n/locales/ru/` (new dir) | **41 JSON files** matching `locales/en/` (includes `hooks.json`, `webhooks.json`, `workstations.json` — which `ko` omits). |
| `ui/web/src/i18n/index.ts` | Add `ru` import block (41 imports), `ru: {...}` resources object (41 namespaces), `stored === "ru"` in `getInitialLanguage()`, and `lang.startsWith("ru") → "ru"` browser detection. |
| `ui/web/src/lib/constants.ts` | Add `"ru"` to `SUPPORTED_LANGUAGES`; add `ru: "Русский"` to `LANGUAGE_LABELS`. |
| `ui/web/src/pages/config/sections/system-messages-section-utils.ts:21` | Add `{ code: "ru", labelKey: "systemMessages.locale.ru" }` to `SYSTEM_MESSAGE_LOCALES`. |
| `ui/web/src/pages/config/sections/system-messages-section-utils.test.ts:49` | Update expected array to include `"ru"`. |
| `ui/web/src/i18n/locales/{en,vi,zh,ru}/config.json` | Add `systemMessages.locale.ru` string key. |

Consumers auto-covered (no edit): `topbar.tsx`, `setup-page.tsx` `LanguageSelector`,
`use-ui-store.ts` — all map over `SUPPORTED_LANGUAGES`/`LANGUAGE_LABELS`.

### 3. Desktop UI — net-new (16 namespaces, patterned on `zh`)

| File | Change |
|------|--------|
| `ui/desktop/frontend/src/i18n/locales/ru/` (new dir) | **16 JSON files** matching `locales/zh/`. |
| `ui/desktop/frontend/src/i18n/index.ts` | Add `ru` import block (16 imports), `ru: {...}` resources, `ru` handling in `getInitialLanguage()`. |
| `ui/desktop/frontend/src/lib/constants.ts` | Add `{ value: 'ru', label: 'RU', flag: '🇷🇺' }` to `LANGUAGES`. |
| `ui/desktop/frontend/src/components/settings/AppearanceTab.tsx` | Add `ru` to inline `LANGUAGES` array. |
| `ui/desktop/frontend/src/components/onboarding/OnboardingWizard.tsx` | Add `ru` to inline `LANGUAGES` array. |

`ChatTopBar.tsx` imports `LANGUAGES` from constants — auto-covered.

**No DB migration:** locale strings live in Go catalogs and JSON files, not in any
PostgreSQL/SQLite table. SQLite `schema.sql`/`schema.go` untouched.

## Translation strategy

- Translate **from the English source** (the parity reference), not from `ko`/`vi`/`zh`.
- Keep technical terms natural/idiomatic: agent→агент, token→токен, session→сессия,
  team→команда, provider→провайдер, skill→навык (or keep "скилл" where UI-conventional).
- **Never touch** `%s` / `%d` (Go `fmt` verbs), `{{variable}}` (i18next interpolation),
  JSON keys, HTML tags, or Markdown structure inside strings.
- Preserve pluralization keys (`_one`/`_other`) where i18next uses them.

## Ordering

1. **Plumbing first:** register `ru` everywhere (backend consts, index.ts, constants,
   enums, selectors) — with English-copied strings as placeholders so it compiles/renders.
2. **Translation second:** replace placeholder strings namespace-by-namespace.

This keeps every intermediate commit buildable.

## Verification

- Backend: `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`,
  `go test ./internal/i18n/ ./internal/systemmessages/`.
- Web: `cd ui/web && pnpm build`; key-parity check (every `en/*.json` key exists in
  `ru/*.json`).
- Desktop: `cd ui/desktop/frontend && pnpm build`; same key-parity check against `zh`.
- Manual smoke: launch web UI, switch language to "Русский" in topbar, verify no raw keys
  render on main screens.

## Out of scope

- Bootstrap templates (`SOUL.md`, `IDENTITY.md`) stay English (LLM consumption).
- Prose `_readmes/README.ru.md` already exists — unrelated to UI wiring.
- No new features; wiring + translation only.
