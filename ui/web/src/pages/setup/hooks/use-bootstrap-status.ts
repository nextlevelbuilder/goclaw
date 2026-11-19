import { useMemo } from "react";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useChatGPTOAuthProviderStatuses } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useAuthStore } from "@/stores/use-auth-store";
import { isSetupSkipped } from "@/lib/setup-skip";
import { resolveBootstrapState } from "./resolve-bootstrap-state";

export type { SetupStep } from "./resolve-bootstrap-state";

export function useBootstrapStatus() {
  const connected = useAuthStore((s) => s.connected);
  const userId = useAuthStore((s) => s.userId);
  const tenantId = useAuthStore((s) => s.tenantId);
  const tenantSlug = useAuthStore((s) => s.tenantSlug);
  const role = useAuthStore((s) => s.role);
  // /v1/providers is admin-only, and only admins can finish the setup wizard.
  const canManageProviders = role === "admin" || role === "owner";
  const { providers, loading: providersLoading, loadFailed: providersLoadFailed } = useProviders(canManageProviders);
  const { statuses: oauthStatuses, isLoading: oauthStatusesLoading } = useChatGPTOAuthProviderStatuses(providers, canManageProviders);
  const { agents, loading: agentsLoading } = useAgents();

  // Wait for WS to connect before considering agents loaded
  const loading = providersLoading || agentsLoading || oauthStatusesLoading || !connected;

  const { needsSetup, currentStep } = useMemo(() => {
    if (loading) return { needsSetup: false, currentStep: "complete" as const };

    const readyOAuthProviders = new Set(
      oauthStatuses
        .filter((status) => status.availability === "ready")
        .map((status) => status.provider.name),
    );
    const hasProvider = providers.some((provider) => provider.enabled && (
      provider.api_key === "***"
      || provider.provider_type === "claude_cli"
      || provider.provider_type === "ollama"
      || (provider.provider_type === "chatgpt_oauth" && readyOAuthProviders.has(provider.name))
    ));

    return resolveBootstrapState({
      canManageProviders,
      providersLoadFailed,
      // Allow skipping setup entirely via localStorage
      skipped: isSetupSkipped({ userId, tenantId, tenantSlug }),
      hasProvider,
      hasAgent: agents.length > 0,
    });
  }, [agents, canManageProviders, loading, oauthStatuses, providers, providersLoadFailed, tenantId, tenantSlug, userId]);

  return { needsSetup, currentStep, loading, providers, agents };
}
