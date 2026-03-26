import type { EffectiveChatGPTOAuthRoutingStrategy } from "@/types/agent";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";

export interface CodexPoolProviderCount {
  provider_name: string;
  request_count: number;
  direct_selection_count: number;
  failover_serve_count: number;
  success_count: number;
  failure_count: number;
  consecutive_failures: number;
  success_rate: number;
  health_score: number;
  health_state: "healthy" | "degraded" | "critical" | "idle";
  last_selected_at?: string;
  last_failover_at?: string;
  last_used_at?: string;
  last_success_at?: string;
  last_failure_at?: string;
}

export interface CodexPoolRecentRequest {
  span_id: string;
  trace_id: string;
  started_at: string;
  status: string;
  duration_ms: number;
  provider_name: string;
  selected_provider?: string;
  model: string;
  attempt_count: number;
  used_failover: boolean;
  failover_providers?: string[];
}

interface CodexPoolActivityResponse {
  strategy: EffectiveChatGPTOAuthRoutingStrategy;
  pool_providers: string[];
  stats_sample_size: number;
  provider_counts: CodexPoolProviderCount[];
  recent_requests: CodexPoolRecentRequest[];
}

export function useCodexPoolActivity(agentId: string, limit = 18, enabled = true) {
  const http = useHttp();

  const query = useQuery({
    queryKey: queryKeys.agents.codexPoolActivity(agentId, limit),
    enabled: enabled && Boolean(agentId),
    staleTime: 5_000,
    queryFn: () => http.get<CodexPoolActivityResponse>(`/v1/agents/${agentId}/codex-pool-activity`, {
      limit: String(limit),
    }),
  });

  return {
    ...query,
    data: query.data ?? {
      strategy: "primary_first" as const,
      pool_providers: [],
      stats_sample_size: 0,
      provider_counts: [],
      recent_requests: [],
    },
  };
}
