import { useMemo } from "react";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useAuthStore } from "@/stores/use-auth-store";

export type SetupStep = 1 | 2 | 3 | 4 | "complete";

export function useBootstrapStatus() {
  const connected = useAuthStore((s) => s.connected);
  const { providers, loading: providersLoading } = useProviders();
  const { agents, loading: agentsLoading } = useAgents();

  // Wait for WS to connect before considering agents loaded
  const loading = providersLoading || agentsLoading || !connected;

  const { needsSetup, currentStep } = useMemo(() => {
    if (loading) return { needsSetup: false, currentStep: "complete" as SetupStep };

    // A provider is "configured" if enabled + has an API key set (masked as "***")
    const hasProvider = providers.some((p) => p.enabled && p.api_key === "***");
    const hasAgent = agents.length > 0;

    if (!hasProvider) return { needsSetup: true, currentStep: 1 as SetupStep };
    if (!hasAgent) return { needsSetup: true, currentStep: 2 as SetupStep };
    return { needsSetup: false, currentStep: "complete" as SetupStep };
  }, [loading, providers, agents]);

  return { needsSetup, currentStep, loading, providers, agents };
}
