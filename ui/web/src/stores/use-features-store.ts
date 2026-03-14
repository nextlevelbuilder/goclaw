import { create } from "zustand";

/** Feature flag names matching the backend FeaturesConfig JSON keys. */
export type FeatureName =
    | "overview" | "chat" | "agents" | "agent_teams"
    | "sessions" | "pending_messages" | "contacts"
    | "channels" | "nodes"
    | "skills" | "builtin_tools" | "mcp_servers" | "tts" | "cron"
    | "memory" | "knowledge_graph" | "storage"
    | "traces" | "events" | "delegations" | "activity" | "logs"
    | "providers" | "config" | "approvals";

export type FeaturesMap = Partial<Record<FeatureName, boolean | null>>;

interface FeaturesState {
    /** The raw feature flags from the server. null/undefined = enabled. */
    features: FeaturesMap;
    /** The user's role from the connect response. */
    role: string;

    /** True if the given feature is enabled. Admin always returns true. */
    isFeatureEnabled: (name: FeatureName) => boolean;

    /** Update features from connect response or features.get call. */
    setFeatures: (features: FeaturesMap) => void;

    /** Set the role (from connect response). */
    setRole: (role: string) => void;
}

export const useFeaturesStore = create<FeaturesState>((set, get) => ({
    features: {},
    role: "",

    isFeatureEnabled: (name: FeatureName) => {
        const { role, features } = get();
        // Admin users always have full access
        if (role === "admin") return true;
        // nil / undefined / null = enabled (default)
        const val = features[name];
        return val === undefined || val === null || val === true;
    },

    setFeatures: (features: FeaturesMap) => set({ features }),

    setRole: (role: string) => set({ role }),
}));
