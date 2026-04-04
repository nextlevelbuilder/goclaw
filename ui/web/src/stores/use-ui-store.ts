import { create } from "zustand";
import { persist } from "zustand/middleware";
import i18n from "@/i18n";
import { type Language } from "@/lib/constants";

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

export const useUiStore = create<UiState>()(
  persist(
    (set, get) => ({
      theme: "dark" as Theme,
      language: (i18n.language as Language) ?? "en",
      timezone: "auto",
      sidebarCollapsed: false,
      mobileSidebarOpen: false,

      setTheme: (theme) => {
        set({ theme });
      },

      setLanguage: (language) => {
        i18n.changeLanguage(language);
        set({ language });
      },

      setTimezone: (tz) => {
        set({ timezone: tz });
      },

      toggleSidebar: () => {
        set({ sidebarCollapsed: !get().sidebarCollapsed });
      },

      setSidebarCollapsed: (collapsed) => {
        set({ sidebarCollapsed: collapsed });
      },

      setMobileSidebarOpen: (open) => set({ mobileSidebarOpen: open }),
    }),
    {
      name: "goclaw:ui", // localStorage key
      partialize: (state) => ({
        // Persist user preferences — not transient UI state
        theme: state.theme,
        language: state.language,
        timezone: state.timezone,
        sidebarCollapsed: state.sidebarCollapsed,
      }),
    }
  )
);
