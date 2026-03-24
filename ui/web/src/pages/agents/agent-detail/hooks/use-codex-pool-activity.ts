import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";

export interface CodexPoolProviderCount {
  provider_name: string;
  request_count: number;
  last_used_at?: string;
}

export interface CodexPoolRecentRequest {
  trace_id: string;
  started_at: string;
  status: string;
  duration_ms: number;
  provider_name: string;
  model: string;
  pool_llm_calls: number;
  failover_providers?: string[];
}

interface CodexPoolActivityResponse {
  strategy: "manual" | "round_robin";
  pool_providers: string[];
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
      strategy: "manual" as const,
      pool_providers: [],
      provider_counts: [],
      recent_requests: [],
    },
  };
}
