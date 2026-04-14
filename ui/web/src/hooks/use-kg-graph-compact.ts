import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import type { KGGraphCompactResponse } from "@/types/graph-dto";

const KG_GRAPH_KEY = "kg-graph-compact";

/** Fetches KG graph from dedicated compact endpoint. */
export function useKGGraphCompact(agentId: string, opts?: { userId?: string; limit?: number }) {
  const http = useHttp();
  const limit = opts?.limit ?? 2000;

  const params = useMemo(() => {
    const p: Record<string, string> = { limit: String(limit) };
    if (opts?.userId) p.user_id = opts.userId;
    return p;
  }, [opts?.userId, limit]);

  const { data, isLoading } = useQuery({
    queryKey: [KG_GRAPH_KEY, agentId, params],
    queryFn: () => http.get<KGGraphCompactResponse>(
      `/v1/agents/${agentId}/kg/graph/compact`, params,
    ),
    staleTime: 60_000,
    enabled: !!agentId,
  });

  return {
    nodes: data?.nodes ?? [],
    edges: data?.edges ?? [],
    totalNodes: data?.total_nodes ?? 0,
    totalEdges: data?.total_edges ?? 0,
    loading: isLoading,
  };
}
