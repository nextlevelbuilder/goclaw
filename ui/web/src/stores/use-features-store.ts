import { create } from "zustand";
import { useAuthStore } from "./use-auth-store";
import { ROUTE_FEATURE_MAP } from "@/lib/feature-registry";

type FeaturesMap = Record<string, boolean | undefined>;

interface FeaturesState {
  features: FeaturesMap;
  setFeatures: (features: FeaturesMap) => void;

  /** Check if a feature is visible. Root role bypasses all flags. */
  isFeatureEnabled: (key: string) => boolean;

  /** Check if a route should be visible based on its feature flag. */
  isRouteEnabled: (route: string) => boolean;
}

export const useFeaturesStore = create<FeaturesState>((set, get) => ({
  features: {},

  setFeatures: (features) => set({ features }),

  isFeatureEnabled: (key) => {
    const role = useAuthStore.getState().role;
    if (role === "root") return true;
    const val = get().features[key];
    // undefined/null/true = enabled (opt-out model)
    return val !== false;
  },

  isRouteEnabled: (route) => {
    const featureKey = ROUTE_FEATURE_MAP[route];
    if (!featureKey) return true; // routes without feature flags are always visible
    return get().isFeatureEnabled(featureKey);
  },
}));
