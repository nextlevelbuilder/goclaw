import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "@/stores/use-toast-store";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { EvolutionSuggestion } from "@/types/evolution";

/**
 * Fetches and manages evolution suggestions for an agent.
 * @param status - optional status filter (empty = all)
 */
export function useEvolutionSuggestions(agentId: string, status?: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const params: Record<string, string> = { limit: "100" };
  if (status) params.status = status;

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.evolution.suggestions(agentId, { status: status ?? "" }),
    queryFn: () =>
      http.get<EvolutionSuggestion[]>(`/v1/agents/${agentId}/evolution/suggestions`, params),
  });

  const updateStatus = useCallback(
    async (suggestionId: string, newStatus: "approved" | "rejected" | "rolled_back") => {
      try {
        await http.patch(`/v1/agents/${agentId}/evolution/suggestions/${suggestionId}`, {
          status: newStatus,
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.evolution.suggestions(agentId, { status: status ?? "" }),
        });
        toast.success(`Suggestion ${newStatus}`);
      } catch {
        toast.error("Failed to update suggestion");
      }
    },
    [http, agentId, status, queryClient],
  );

  return {
    suggestions: data ?? [],
    loading: isLoading,
    updateStatus,
  };
}
