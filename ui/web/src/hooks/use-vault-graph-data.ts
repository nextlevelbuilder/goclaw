import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import type { VaultGraphResponse } from "@/types/graph-dto";

const VAULT_GRAPH_KEY = "vault-graph";

interface VaultGraphOpts {
  teamId?: string;
  limit?: number;
}

/** Fetches vault graph data from dedicated lightweight endpoint.
 *  Single request returns nodes (with degree) + edges. */
export function useVaultGraphData(agentId: string, opts?: VaultGraphOpts) {
  const http = useHttp();
  const limit = opts?.limit ?? 2000;

  const params = useMemo(() => {
    const p: Record<string, string> = { limit: String(limit) };
    if (agentId) p.agent_id = agentId;
    if (opts?.teamId) p.team_id = opts.teamId;
    return p;
  }, [agentId, opts?.teamId, limit]);

  const { data, isLoading } = useQuery({
    queryKey: [VAULT_GRAPH_KEY, params],
    queryFn: () => http.get<VaultGraphResponse>("/v1/vault/graph", params),
    staleTime: 60_000,
  });

  return {
    nodes: data?.nodes ?? [],
    edges: data?.edges ?? [],
    totalNodes: data?.total_nodes ?? 0,
    totalEdges: data?.total_edges ?? 0,
    loading: isLoading,
  };
}
