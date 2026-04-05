import { create } from "zustand";

interface BrowserViewState {
  /** Target ID of the browser tab to show, null = no active browser */
  targetId: string | null;
  tabTitle: string | null;
  tabUrl: string | null;
  /** Whether the inline panel is visible (can be toggled without losing targetId) */
  panelVisible: boolean;
  openBrowserView: (targetId: string, title?: string, url?: string) => void;
  closeBrowserView: () => void;
  togglePanel: () => void;
  showPanel: () => void;
  hidePanel: () => void;
}

export const useBrowserViewStore = create<BrowserViewState>((set, get) => ({
  targetId: null,
  tabTitle: null,
  tabUrl: null,
  panelVisible: false,
  openBrowserView: (targetId, title, url) => {
    const s = get();
    // Skip state update when targetId is already active — prevents unnecessary
    // re-renders that could unmount/remount the panel and kill the WS connection.
    if (s.targetId === targetId && s.panelVisible) return;
    set({ targetId, tabTitle: title ?? null, tabUrl: url ?? null, panelVisible: true });
  },
  closeBrowserView: () =>
    set({ targetId: null, tabTitle: null, tabUrl: null, panelVisible: false }),
  togglePanel: () => {
    const { targetId, panelVisible } = get();
    if (targetId) set({ panelVisible: !panelVisible });
  },
  showPanel: () => {
    if (get().targetId) set({ panelVisible: true });
  },
  hidePanel: () => set({ panelVisible: false }),
}));
