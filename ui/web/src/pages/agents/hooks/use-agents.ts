import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs, useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import { userFriendlyError } from "@/lib/error-utils";
import type { AgentData } from "@/types/agent";

interface AgentInfoWs {
  id: string;
  model: string;
  isRunning: boolean;
}

export function useAgents() {
  const ws = useWs();
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const queryClient = useQueryClient();

  const { data: agents = [], isPending: loading, error: queryError } = useQuery({
    queryKey: queryKeys.agents.all,
    queryFn: async () => {
      // Try HTTP first (returns full agent data, filtered by user access)
      try {
        const res = await http.get<{ agents: AgentData[] }>("/v1/agents");
        if (res.agents && res.agents.length > 0) {
          return res.agents;
        }
      } catch {
        // HTTP may fail if user doesn't have access - fall through to WS
      }

      // Fallback: WS agents.list returns all running agents (no access filter)
      if (!ws.isConnected) return [];
      const res = await ws.call<{ agents: AgentInfoWs[] }>(Methods.AGENTS_LIST);
      return (res.agents ?? []).map((a): AgentData => ({
        id: a.id,
        agent_key: a.id,
        owner_id: "",
        provider: "",
        model: a.model,
        context_window: 0,
        max_tool_iterations: 0,
        workspace: "",
        restrict_to_workspace: false,
        agent_type: (a as unknown as { agentType?: string }).agentType === "predefined" ? "predefined" as const : "open" as const,
        is_default: false,
        status: a.isRunning ? "active" : "inactive",
      }));
    },
    staleTime: 60_000,
    enabled: connected,
  });

  const error = queryError instanceof Error ? queryError.message : queryError ? "Failed to load agents" : null;

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.agents.all }),
    [queryClient],
  );

  const createAgent = useCallback(
    async (data: Partial<AgentData>) => {
      try {
        const res = await http.post<AgentData>("/v1/agents", data);
        await invalidate();
        toast.success(i18n.t("agents:toast.created"), `${data.display_name || data.agent_key || "Agent"} has been added`);
        return res;
      } catch (err) {
        toast.error(i18n.t("agents:toast.createFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const updateAgent = useCallback(
    async (id: string, data: Partial<AgentData>) => {
      try {
        await http.put(`/v1/agents/${id}`, data);
        await invalidate();
        toast.success(i18n.t("agents:toast.updated"), `${data.display_name || data.agent_key || "Agent"} has been updated`);
      } catch (err) {
        toast.error(i18n.t("agents:toast.updateFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteAgent = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/agents/${id}`);
        await invalidate();
        toast.success(i18n.t("agents:toast.deleted"));
      } catch (err) {
        toast.error(i18n.t("agents:toast.deleteFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const resummonAgent = useCallback(
    async (id: string) => {
      await http.post(`/v1/agents/${id}/resummon`);
    },
    [http],
  );

  const cancelSummonAgent = useCallback(
    async (id: string) => {
      await http.post(`/v1/agents/${id}/cancel-summon`);
    },
    [http],
  );

  return { agents, loading, error, refresh: invalidate, createAgent, updateAgent, deleteAgent, resummonAgent, cancelSummonAgent };
}
