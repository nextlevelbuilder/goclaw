import { create } from "zustand";
import i18n from "@/i18n";
import { LOCAL_STORAGE_KEYS, SUPPORTED_LANGUAGES, type Language } from "@/lib/constants";

export type Theme = "light" | "dark" | "system";

interface UiState {
  theme: Theme;
  lockedTheme: "light" | "dark" | null;
  language: Language;
  lockedLanguage: Language | null;
  timezone: string; // IANA timezone or "auto"
  lockedTimezone: string | null;
  sidebarCollapsed: boolean;
  mobileSidebarOpen: boolean;

  setTheme: (theme: Theme) => void;
  setLockedTheme: (theme: "light" | "dark" | null) => void;
  setLanguage: (language: Language) => void;
  setLockedLanguage: (language: Language | null) => void;
  setTimezone: (tz: string) => void;
  setLockedTimezone: (tz: string | null) => void;
  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setMobileSidebarOpen: (open: boolean) => void;
}

export const useUiStore = create<UiState>((set) => ({
  theme: (localStorage.getItem(LOCAL_STORAGE_KEYS.THEME) as Theme) ?? "dark",
  lockedTheme: null,
  language: (i18n.language as Language) ?? "en",
  lockedLanguage: null,
  timezone: localStorage.getItem(LOCAL_STORAGE_KEYS.TIMEZONE) ?? "auto",
  lockedTimezone: null,
  sidebarCollapsed:
    localStorage.getItem(LOCAL_STORAGE_KEYS.SIDEBAR_COLLAPSED) === "true",
  mobileSidebarOpen: false,

  setTheme: (theme) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.THEME, theme);
    set({ theme });
  },

  setLockedTheme: (theme) => set({ lockedTheme: theme }),

  setLanguage: (language) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.LANGUAGE, language);
    i18n.changeLanguage(language);
    set({ language });
  },

  setLockedLanguage: (language) => {
    if (language && (SUPPORTED_LANGUAGES as readonly string[]).includes(language)) {
      i18n.changeLanguage(language);
      set({ lockedLanguage: language, language });
    } else {
      set({ lockedLanguage: null });
    }
  },

  setTimezone: (tz) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.TIMEZONE, tz);
    set({ timezone: tz });
  },

  setLockedTimezone: (tz) => {
    if (tz) {
      set({ lockedTimezone: tz, timezone: tz });
    } else {
      set({ lockedTimezone: null });
    }
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
