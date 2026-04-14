import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import type { EpisodicSummary, EpisodicSearchResult } from "@/types/memory";

const EPISODIC_KEY = "episodic";

/** List episodic summaries for an agent. */
export function useEpisodicSummaries(agentId: string, opts: { userId?: string; limit?: number; offset?: number }) {
  const http = useHttp();

  const params = useMemo(() => {
    const p: Record<string, string> = {};
    if (opts.userId) p.user_id = opts.userId;
    if (opts.limit !== undefined) p.limit = String(opts.limit);
    if (opts.offset !== undefined) p.offset = String(opts.offset);
    return p;
  }, [opts.userId, opts.limit, opts.offset]);

  const { data, isLoading } = useQuery({
    queryKey: [EPISODIC_KEY, agentId, params],
    queryFn: () => http.get<EpisodicSummary[]>(`/v1/agents/${agentId}/episodic`, params),
    staleTime: 60_000,
    enabled: !!agentId,
  });

  return { summaries: data ?? [], loading: isLoading };
}

/** Search episodic summaries. */
export function useEpisodicSearch(agentId: string) {
  const http = useHttp();

  const search = useCallback(
    async (query: string, userId?: string) => {
      try {
        return await http.post<EpisodicSearchResult[]>(`/v1/agents/${agentId}/episodic/search`, {
          query,
          user_id: userId,
          max_results: 20,
        });
      } catch {
        toast.error("Episodic search failed");
        return [];
      }
    },
    [http, agentId],
  );

  return { search };
}
