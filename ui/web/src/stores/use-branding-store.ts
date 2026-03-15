import { create } from "zustand";

interface BrandingState {
  appName: string;
  appDescription: string;
  setBranding: (appName: string, appDescription?: string) => void;
}

export const useBrandingStore = create<BrandingState>((set) => ({
  appName: "GoClaw",
  appDescription: "",
  setBranding: (appName, appDescription = "") =>
    set({ appName: appName || "GoClaw", appDescription }),
}));
