import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type {
  CodexPoolProviderCount,
  CodexPoolRecentRequest,
} from "@/pages/agents/agent-detail/hooks/use-codex-pool-activity";
import type { EffectiveChatGPTOAuthRoutingStrategy } from "@/types/agent";

export interface ProviderCodexPoolAgentCount {
  agent_id: string;
  agent_key?: string;
  request_count: number;
}

interface ProviderCodexPoolActivityResponse {
  strategy: EffectiveChatGPTOAuthRoutingStrategy;
  pool_providers: string[];
  stats_sample_size: number;
  provider_counts: CodexPoolProviderCount[];
  recent_requests: CodexPoolRecentRequest[];
  top_agents: ProviderCodexPoolAgentCount[];
}

const EMPTY: ProviderCodexPoolActivityResponse = {
  strategy: "priority_order",
  pool_providers: [],
  stats_sample_size: 0,
  provider_counts: [],
  recent_requests: [],
  top_agents: [],
};

export function useProviderCodexPoolActivity(
  providerId: string,
  limit = 18,
  enabled = true,
) {
  const http = useHttp();

  const query = useQuery({
    queryKey: queryKeys.providers.codexPoolActivity(providerId, limit),
    enabled: enabled && Boolean(providerId),
    staleTime: 5_000,
    queryFn: () =>
      http.get<ProviderCodexPoolActivityResponse>(
        `/v1/providers/${providerId}/codex-pool-activity`,
        { limit: String(limit) },
      ),
  });

  return {
    ...query,
    data: query.data ?? EMPTY,
  };
}
