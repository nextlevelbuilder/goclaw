import { create } from "zustand";
import i18n from "@/i18n";
import {
  COLOR_PRESETS,
  LOCAL_STORAGE_KEYS,
  type ColorPreset,
  type Language,
} from "@/lib/constants";

export type Theme = "light" | "dark" | "system";

interface UiState {
  theme: Theme;
  colorPreset: ColorPreset;
  language: Language;
  timezone: string; // IANA timezone or "auto"
  sidebarCollapsed: boolean;
  mobileSidebarOpen: boolean;

  setTheme: (theme: Theme) => void;
  setColorPreset: (preset: ColorPreset) => void;
  setLanguage: (language: Language) => void;
  setTimezone: (tz: string) => void;
  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setMobileSidebarOpen: (open: boolean) => void;
}

const savedColorPreset = localStorage.getItem(LOCAL_STORAGE_KEYS.COLOR_PRESET);
const initialColorPreset: ColorPreset = COLOR_PRESETS.includes(
  savedColorPreset as ColorPreset,
)
  ? (savedColorPreset as ColorPreset)
  : "classic";

export const useUiStore = create<UiState>((set) => ({
  theme: (localStorage.getItem(LOCAL_STORAGE_KEYS.THEME) as Theme) ?? "dark",
  colorPreset: initialColorPreset,
  language: (i18n.language as Language) ?? "en",
  timezone: localStorage.getItem(LOCAL_STORAGE_KEYS.TIMEZONE) ?? "auto",
  sidebarCollapsed:
    localStorage.getItem(LOCAL_STORAGE_KEYS.SIDEBAR_COLLAPSED) === "true",
  mobileSidebarOpen: false,

  setTheme: (theme) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.THEME, theme);
    set({ theme });
  },

  setColorPreset: (preset) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.COLOR_PRESET, preset);
    set({ colorPreset: preset });
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
