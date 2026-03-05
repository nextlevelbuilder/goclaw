import { create } from "zustand";
import { LOCAL_STORAGE_KEYS } from "@/lib/constants";

export type Theme = "light" | "dark" | "system";

interface UiState {
  theme: Theme;
  sidebarCollapsed: boolean;
  mobileSidebarOpen: boolean;

  setTheme: (theme: Theme) => void;
  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setMobileSidebarOpen: (open: boolean) => void;
}

export const useUiStore = create<UiState>((set) => ({
  theme: (localStorage.getItem(LOCAL_STORAGE_KEYS.THEME) as Theme) ?? "dark",
  sidebarCollapsed:
    localStorage.getItem(LOCAL_STORAGE_KEYS.SIDEBAR_COLLAPSED) === "true",
  mobileSidebarOpen: false,

  setTheme: (theme) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.THEME, theme);
    set({ theme });
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
