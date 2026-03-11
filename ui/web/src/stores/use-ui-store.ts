import { create } from "zustand";
import i18n from "@/i18n";
import { LOCAL_STORAGE_KEYS, type Language } from "@/lib/constants";

export type Theme = "light" | "dark" | "system";

interface UiState {
  theme: Theme;
  language: Language;
  timezone: string; // IANA timezone or "auto"
  sidebarCollapsed: boolean;
  mobileSidebarOpen: boolean;

  setTheme: (theme: Theme) => void;
  setLanguage: (language: Language) => void;
  setTimezone: (tz: string) => void;
  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setMobileSidebarOpen: (open: boolean) => void;
}

export const useUiStore = create<UiState>((set) => ({
  theme: (localStorage.getItem(LOCAL_STORAGE_KEYS.THEME) as Theme) ?? "dark",
  language: (i18n.language as Language) ?? "en",
  timezone: localStorage.getItem(LOCAL_STORAGE_KEYS.TIMEZONE) ?? "auto",
  sidebarCollapsed:
    localStorage.getItem(LOCAL_STORAGE_KEYS.SIDEBAR_COLLAPSED) === "true",
  mobileSidebarOpen: false,

  setTheme: (theme) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.THEME, theme);
    set({ theme });
  },

  setLanguage: (language) => {
    i18n.changeLanguage(language);
    set({ language });
  },

  setTimezone: (tz) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.TIMEZONE, tz);
    set({ timezone: tz });
  },

  toggleSidebar: () =>
    set((state) => {
      const next = !state.sidebarCollapsed;
      localStorage.setItem(LOCAL_STORAGE_KEYS.SIDEBAR_COLLAPSED, String(next));
      return { sidebarCollapsed: next };
    }),

  setSidebarCollapsed: (collapsed) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.SIDEBAR_COLLAPSED, String(collapsed));
    set({ sidebarCollapsed: collapsed });
  },

  setMobileSidebarOpen: (open) => set({ mobileSidebarOpen: open }),
}));
