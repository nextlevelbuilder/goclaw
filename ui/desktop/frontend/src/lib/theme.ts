export type ThemePreference = 'system' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

const DARK_QUERY = '(prefers-color-scheme: dark)'

/** Whether the OS currently prefers a dark colour scheme. False when matchMedia is unavailable. */
export function systemPrefersDark(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return window.matchMedia(DARK_QUERY).matches
}

/** Resolve a stored preference to the theme that should be painted. */
export function resolveTheme(pref: ThemePreference, prefersDark: boolean = systemPrefersDark()): ResolvedTheme {
  if (pref === 'system') return prefersDark ? 'dark' : 'light'
  return pref
}

/** Toggle the `.dark` class on <html>. */
export function applyTheme(theme: ResolvedTheme): void {
  document.documentElement.classList.toggle('dark', theme === 'dark')
}

/**
 * Subscribe to OS colour-scheme changes. Returns an unsubscribe function.
 * A no-op when matchMedia is unavailable (tests, very old webviews).
 */
export function onSystemThemeChange(listener: (prefersDark: boolean) => void): () => void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return () => {}
  const mql = window.matchMedia(DARK_QUERY)
  const handler = (e: MediaQueryListEvent) => listener(e.matches)
  mql.addEventListener('change', handler)
  return () => mql.removeEventListener('change', handler)
}

/** Narrow an unknown persisted value to a valid preference. */
export function toThemePreference(value: unknown): ThemePreference {
  return value === 'light' || value === 'dark' || value === 'system' ? value : 'system'
}
