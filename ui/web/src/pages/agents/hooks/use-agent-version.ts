import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { V3Flags } from "@/hooks/use-v3-flags";

/** Returns "v3" if v3 pipeline is enabled, "v2" otherwise. Cached per agent. */
export function useAgentVersion(agentId: string): "v2" | "v3" {
  const http = useHttp();
  const { data } = useQuery({
    queryKey: queryKeys.v3Flags.detail(agentId),
    queryFn: () => http.get<V3Flags>(`/v1/agents/${agentId}/v3-flags`),
    enabled: !!agentId,
    staleTime: 60_000, // cache 1 min to avoid N+1 in list views
  });
  return data?.v3_pipeline_enabled ? "v3" : "v2";
}
