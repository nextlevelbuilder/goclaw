import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type AppView = 'chat' | 'settings' | 'team-board'
export type SettingsTab = 'appearance' | 'providers' | 'agents' | 'channels' | 'mcp' | 'skills' | 'tools' | 'cron' | 'traces' | 'storage' | 'about'

interface UiState {
  theme: 'dark' | 'light'
  locale: string
  timezone: string
  sidebarOpen: boolean
  sidebarWidth: number
  onboarded: boolean
  activeView: AppView
  settingsTab: SettingsTab
  activeTeamId: string | null
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
}

export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      theme: 'light',
      locale: 'vi',
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      sidebarOpen: true,
      sidebarWidth: 260,
      onboarded: false,
      activeView: 'chat',
      activeTeamId: null,
      settingsTab: 'appearance',
      toggleTheme: () =>
        set((s) => ({ theme: s.theme === 'dark' ? 'light' : 'dark' })),
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
        set({ settingsTab: tab }),
      openSettings: (tab) =>
        set((s) => ({ activeView: 'settings', settingsTab: tab ?? s.settingsTab })),
      closeSettings: () =>
        set({ activeView: 'chat' }),
      openTeamBoard: (teamId) =>
        set({ activeView: 'team-board', activeTeamId: teamId }),
    }),
    {
      name: 'goclaw-ui',
      partialize: (s) => ({
        theme: s.theme,
        locale: s.locale,
        timezone: s.timezone,
        sidebarOpen: s.sidebarOpen,
        sidebarWidth: s.sidebarWidth,
        onboarded: s.onboarded,
        settingsTab: s.settingsTab,
      }),
    }
  )
)
