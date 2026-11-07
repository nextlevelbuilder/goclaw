import { useSyncExternalStore } from 'react'
import { onSystemThemeChange, resolveTheme, systemPrefersDark, type ResolvedTheme } from '../../lib/theme'
import { useUiStore } from '../../stores/ui-store'

const subscribe = (onChange: () => void) => onSystemThemeChange(() => onChange())

/** The theme actually painted: the user's choice, or the OS setting when the choice is "system". */
export function useResolvedTheme(): ResolvedTheme {
  const preference = useUiStore((s) => s.theme)
  const prefersDark = useSyncExternalStore(subscribe, systemPrefersDark, () => false)
  return resolveTheme(preference, prefersDark)
}
