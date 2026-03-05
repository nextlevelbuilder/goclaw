import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { queryKeys } from "@/lib/query-keys";
import type { AgentData } from "@/types/agent";

/**
 * Build lookup maps from the TanStack Query agents cache.
 * Returns a resolve function: (keyOrId) → display_name | fallback.
 * The cache is already populated by useAgents() which runs on the agents page.
 */
export function useAgentResolver() {
  const queryClient = useQueryClient();
  const agents = queryClient.getQueryData<AgentData[]>(queryKeys.agents.all);

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
