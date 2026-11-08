import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { toThemePreference, type ThemePreference } from '../lib/theme'

/** Every top-level view the shell can show. `team-board` is reached from Teams. */
export const APP_VIEWS = [
  'chat',
  'sessions',
  'teams',
  'team-board',
  'agents',
  'skills',
  'tools',
  'providers',
  'mcp',
  'channels',
  'storage',
  'traces',
  'usage',
  'cron',
  'settings',
] as const
export type AppView = (typeof APP_VIEWS)[number]

export function isAppView(value: unknown): value is AppView {
  return typeof value === 'string' && (APP_VIEWS as readonly string[]).includes(value)
}

/**
 * Settings sub-pages. Only `appearance` and `about` still live inside Settings;
 * the other names are kept so existing callers keep working and are redirected
 * to their own sidebar view.
 */
export type SettingsTab = 'appearance' | 'providers' | 'agents' | 'channels' | 'mcp' | 'skills' | 'tools' | 'cron' | 'traces' | 'storage' | 'about'
export type SettingsPage = 'appearance' | 'about'

function isSettingsPage(tab: SettingsTab): tab is SettingsPage {
  return tab === 'appearance' || tab === 'about'
}

export type { ThemePreference }

interface UiState {
  theme: ThemePreference
  locale: string
  timezone: string
  sidebarOpen: boolean
  sidebarWidth: number
  onboarded: boolean
  activeView: AppView
  settingsTab: SettingsTab
  /** Bumped by every openSettings call so a mounted Settings view can re-select its section. */
  settingsNonce: number
  activeTeamId: string | null
  commandPaletteOpen: boolean
  setTheme: (theme: ThemePreference) => void
  /** Flip between light and dark, starting from the theme currently painted. */
  toggleTheme: () => void
  setLocale: (locale: string) => void
  setTimezone: (tz: string) => void
  toggleSidebar: () => void
  setSidebarWidth: (width: number) => void
  completeOnboarding: () => void
  resetOnboarding: () => void
  setActiveView: (view: AppView) => void
  setSettingsTab: (tab: SettingsTab) => void
  openSettings: (tab?: SettingsTab) => void
  closeSettings: () => void
  openTeamBoard: (teamId: string) => void
  setCommandPaletteOpen: (open: boolean) => void
}

/** Route a settings tab either to a Settings page or to its own view. */
function settingsTarget(tab: SettingsTab, current: SettingsTab): Pick<UiState, 'activeView' | 'settingsTab'> {
  if (isSettingsPage(tab)) return { activeView: 'settings', settingsTab: tab }
  return { activeView: tab, settingsTab: current }
}

function paintedThemeIsDark(): boolean {
  return typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
}

type PersistedUiState = Pick<UiState, 'theme' | 'locale' | 'timezone' | 'sidebarOpen' | 'sidebarWidth' | 'onboarded' | 'settingsTab'>

export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      theme: 'system',
      locale: 'vi',
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      sidebarOpen: true,
      sidebarWidth: 264,
      onboarded: false,
      activeView: 'chat',
      activeTeamId: null,
      settingsTab: 'appearance',
      settingsNonce: 0,
      commandPaletteOpen: false,
      setTheme: (theme) => set({ theme }),
      toggleTheme: () =>
        set((s) => {
          const dark = s.theme === 'system' ? paintedThemeIsDark() : s.theme === 'dark'
          return { theme: dark ? 'light' : 'dark' }
        }),
      setLocale: (locale) =>
        set({ locale }),
      setTimezone: (tz) =>
        set({ timezone: tz }),
      toggleSidebar: () =>
        set((s) => ({ sidebarOpen: !s.sidebarOpen })),
      setSidebarWidth: (width) =>
        set({ sidebarWidth: width }),
      completeOnboarding: () =>
        set({ onboarded: true }),
      resetOnboarding: () =>
        set({ onboarded: false }),
      setActiveView: (view) =>
        set({ activeView: view }),
      setSettingsTab: (tab) =>
        set((s) => settingsTarget(tab, s.settingsTab)),
      openSettings: (tab) =>
        set((s) => ({ ...settingsTarget(tab ?? s.settingsTab, s.settingsTab), settingsNonce: s.settingsNonce + 1 })),
      closeSettings: () =>
        set({ activeView: 'chat' }),
      openTeamBoard: (teamId) =>
        set({ activeView: 'team-board', activeTeamId: teamId }),
      setCommandPaletteOpen: (open) =>
        set({ commandPaletteOpen: open }),
    }),
    {
      name: 'goclaw-ui',
      version: 1,
      partialize: (s) => ({
        theme: s.theme,
        locale: s.locale,
        timezone: s.timezone,
        sidebarOpen: s.sidebarOpen,
        sidebarWidth: s.sidebarWidth,
        onboarded: s.onboarded,
        settingsTab: s.settingsTab,
      }),
      // The migrated object may lack fields; persist merges it over the defaults.
      migrate: (persisted) => migrateUiState(persisted) as PersistedUiState,
    }
  )
)

/**
 * Upgrade persisted state from older versions: keep an explicit light/dark
 * choice, fall back to `system` for anything else, and reset a settings tab that
 * now has its own view back to `appearance`.
 */
export function migrateUiState(persisted: unknown): Partial<PersistedUiState> {
  const state = (persisted && typeof persisted === 'object' ? persisted : {}) as Record<string, unknown>
  const tab = state.settingsTab
  return {
    ...(state as Partial<PersistedUiState>),
    theme: toThemePreference(state.theme),
    settingsTab: tab === 'about' ? 'about' : 'appearance',
  }
}
