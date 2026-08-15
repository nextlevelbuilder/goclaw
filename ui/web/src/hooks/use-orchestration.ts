import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";

export interface DelegateTarget {
  agent_key: string;
  display_name: string;
}

export interface OrchestrationInfo {
  mode: "spawn" | "delegate" | "team";
  delegate_targets: DelegateTarget[];
  team: { id: string; name: string } | null;
}

export function useOrchestration(agentId: string) {
  const http = useHttp();

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.orchestration.detail(agentId),
    queryFn: () => http.get<OrchestrationInfo>(`/v1/agents/${agentId}/orchestration`),
    staleTime: 60_000,
  });

  return {
    mode: data?.mode ?? "spawn",
    delegateTargets: data?.delegate_targets ?? [],
    team: data?.team ?? null,
    loading: isLoading,
  };
}
