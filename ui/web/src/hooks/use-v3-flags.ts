import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "@/stores/use-toast-store";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";

export interface V3Flags {
  v3_pipeline_enabled: boolean;
  v3_memory_enabled: boolean;
  v3_retrieval_enabled: boolean;
  self_evolution_metrics: boolean;
  self_evolution_suggestions: boolean;
}

export function useV3Flags(agentId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: flags, isLoading } = useQuery({
    queryKey: queryKeys.v3Flags.detail(agentId),
    queryFn: () => http.get<V3Flags>(`/v1/agents/${agentId}/v3-flags`),
    staleTime: 60_000,
  });

  const toggleFlag = useCallback(
    async (key: keyof V3Flags, value: boolean) => {
      try {
        queryClient.setQueryData<V3Flags>(queryKeys.v3Flags.detail(agentId), (old) =>
          old ? { ...old, [key]: value } : old,
        );
        await http.patch(`/v1/agents/${agentId}/v3-flags`, { [key]: value });
        queryClient.invalidateQueries({ queryKey: queryKeys.v3Flags.detail(agentId) });
      } catch {
        toast.error("Failed to update flag");
      }
    },
    [http, agentId, queryClient],
  );

  return { flags: flags ?? null, loading: isLoading, toggleFlag };
}
