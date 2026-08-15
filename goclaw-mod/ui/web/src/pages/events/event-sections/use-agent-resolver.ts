import { useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { queryKeys } from "@/lib/query-keys";
import type { AgentData } from "@/types/agent";

/**
 * Build lookup maps from agents data.
 * Actively fetches agents if cache is empty (the Agents page may not
 * have been visited yet).
 */
export function useAgentResolver() {
  const queryClient = useQueryClient();
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);

  const { data: agents } = useQuery({
    queryKey: queryKeys.agents.all,
    queryFn: async () => {
      const res = await http.get<{ agents: AgentData[] }>("/v1/agents");
      return res.agents ?? [];
    },
    enabled: connected,
    staleTime: 60_000,
    initialData: () => queryClient.getQueryData<AgentData[]>(queryKeys.agents.all),
  });

  const { byKey, byId } = useMemo(() => {
    const byKey = new Map<string, AgentData>();
    const byId = new Map<string, AgentData>();
    for (const a of agents ?? []) {
      if (a.agent_key) byKey.set(a.agent_key, a);
      if (a.id) byId.set(a.id, a);
    }
    return { byKey, byId };
  }, [agents]);

  /** Resolve agent_key or UUID to display name. Falls back to the input string. */
  const resolveAgent = (keyOrId: string | undefined): string => {
    if (!keyOrId) return "";
    const agent = byKey.get(keyOrId) ?? byId.get(keyOrId);
    return agent?.display_name || agent?.agent_key || keyOrId;
  };

  return { resolveAgent };
}
