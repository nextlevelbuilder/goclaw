import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp, useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import { userFriendlyError } from "@/lib/error-utils";
import type { AgentData, BootstrapFile } from "@/types/agent";

interface AgentDetailData {
  agent: AgentData;
  files: BootstrapFile[];
}

export function useAgentDetail(agentId: string | undefined) {
  const http = useHttp();
  const ws = useWs();
  const queryClient = useQueryClient();

  const { data, isLoading: loading } = useQuery({
    queryKey: queryKeys.agents.detail(agentId ?? ""),
    queryFn: async (): Promise<AgentDetailData> => {
      // Try HTTP first (may fail with 403 if user isn't owner/shared)
      let ag: AgentData;
      try {
        ag = await http.get<AgentData>(`/v1/agents/${agentId}`);
      } catch {
        // HTTP failed - construct minimal agent from agentId (which is the agent_key)
        ag = {
          id: agentId!,
          agent_key: agentId!,
          owner_id: "",
          provider: "",
          model: "",
          context_window: 0,
          max_tool_iterations: 0,
          workspace: "",
          restrict_to_workspace: false,
          agent_type: "open" as const,
          is_default: false,
          status: "active",
        };
      }

      // Load files via WS (no access control)
      let files: BootstrapFile[] = [];
      if (ws.isConnected) {
        try {
          const filesRes = await ws.call<{ files: BootstrapFile[] }>(
            Methods.AGENTS_FILES_LIST,
            { agentId: ag.agent_key },
          );
          files = filesRes.files ?? [];
        } catch {
          // ignore
        }
      }

      return { agent: ag, files };
    },
    enabled: !!agentId,
  });

  const agent = data?.agent ?? null;
  const files = data?.files ?? [];

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: queryKeys.agents.detail(agentId ?? "") });
    queryClient.invalidateQueries({ queryKey: queryKeys.agents.all });
  }, [queryClient, agentId]);

  const updateAgent = useCallback(
    async (updates: Record<string, unknown>) => {
      if (!agentId) return;
      try {
        await http.put(`/v1/agents/${agentId}`, updates);
        await invalidate();
        toast.success(i18n.t("agents:toast.updated"));
      } catch (err) {
        toast.error(i18n.t("agents:toast.updateFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [agentId, http, invalidate],
  );

  const getFile = useCallback(
    async (name: string): Promise<BootstrapFile | null> => {
      if (!agent || !ws.isConnected) return null;
      const res = await ws.call<{ file: BootstrapFile }>(Methods.AGENTS_FILES_GET, {
        agentId: agent.agent_key,
        name,
      });
      return res.file;
    },
    [agent, ws],
  );

  const setFile = useCallback(
    async (name: string, content: string) => {
      if (!agent || !ws.isConnected) return;
      try {
        await ws.call(Methods.AGENTS_FILES_SET, {
          agentId: agent.agent_key,
          name,
          content,
        });
        await invalidate();
        toast.success(i18n.t("agents:toast.updated"));
      } catch (err) {
        toast.error(i18n.t("agents:toast.updateFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [agent, ws, invalidate],
  );

  const regenerateAgent = useCallback(
    async (prompt: string) => {
      if (!agentId) return;
      await http.post(`/v1/agents/${agentId}/regenerate`, { prompt });
    },
    [agentId, http],
  );

  const resummonAgent = useCallback(async () => {
    if (!agentId) return;
    await http.post(`/v1/agents/${agentId}/resummon`);
  }, [agentId, http]);

  const deleteAgent = useCallback(async () => {
    if (!agentId) return;
    await http.delete(`/v1/agents/${agentId}`);
    queryClient.invalidateQueries({ queryKey: queryKeys.agents.all });
  }, [agentId, http, queryClient]);

  return { agent, files, loading, updateAgent, getFile, setFile, regenerateAgent, resummonAgent, deleteAgent, refresh: invalidate };
}
