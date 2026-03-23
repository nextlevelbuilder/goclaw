import { useMemo } from "react";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useAuthStore } from "@/stores/use-auth-store";
import { isSetupSkipped } from "@/lib/setup-skip";

export type SetupStep = 1 | 2 | 3 | 4 | "complete";

export function useBootstrapStatus() {
  const connected = useAuthStore((s) => s.connected);
  const userId = useAuthStore((s) => s.userId);
  const tenantId = useAuthStore((s) => s.tenantId);
  const tenantSlug = useAuthStore((s) => s.tenantSlug);
  const { providers, loading: providersLoading } = useProviders();
  const { agents, loading: agentsLoading } = useAgents();

  // Wait for WS to connect before considering agents loaded
  const loading = providersLoading || agentsLoading || !connected;

  const { needsSetup, currentStep } = useMemo(() => {
    if (loading) return { needsSetup: false, currentStep: "complete" as SetupStep };

    // A provider is "configured" if enabled + has an API key set (masked as "***")
    // Claude CLI, ChatGPT OAuth, and local Ollama don't require API keys — check type instead
    const hasProvider = providers.some((p) => p.enabled &&
      (p.api_key === "***" || p.provider_type === "claude_cli" || p.provider_type === "chatgpt_oauth" || p.provider_type === "ollama"));
    const hasAgent = agents.length > 0;

    // Allow skipping setup entirely via localStorage
    const skipped = isSetupSkipped({ userId, tenantId, tenantSlug });
    if (skipped) return { needsSetup: false, currentStep: "complete" as SetupStep };

    if (!hasProvider) return { needsSetup: true, currentStep: 1 as SetupStep };
    if (!hasAgent) return { needsSetup: true, currentStep: 2 as SetupStep };
    return { needsSetup: false, currentStep: "complete" as SetupStep };
  }, [loading, providers, agents, userId, tenantId, tenantSlug]);

  return { needsSetup, currentStep, loading, providers, agents };
}
