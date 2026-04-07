import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { AggregatedMetrics } from "@/types/evolution";

/**
 * Fetches aggregated evolution metrics (tool + retrieval) for an agent.
 * @param timeRange - "7d" | "30d" | "90d"
 */
export function useEvolutionMetrics(agentId: string, timeRange: string) {
  const http = useHttp();

  // Floor to start-of-day for stable cache key across re-renders.
  const since = useMemo(() => {
    const d = new Date();
    d.setDate(d.getDate() - (timeRange === "90d" ? 90 : timeRange === "30d" ? 30 : 7));
    d.setHours(0, 0, 0, 0);
    return d.toISOString();
  }, [timeRange]);

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.evolution.metrics(agentId, { timeRange }),
    queryFn: () =>
      http.get<AggregatedMetrics>(`/v1/agents/${agentId}/evolution/metrics`, {
        aggregate: "true",
        since,
      }),
  });

  return {
    toolAggs: data?.tool_aggregates ?? [],
    retrievalAggs: data?.retrieval_aggregates ?? [],
    loading: isLoading,
  };
}
