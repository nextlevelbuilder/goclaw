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
        agent_type: "open" as const,
        is_default: false,
        status: a.isRunning ? "running" : "idle",
      }));
    },
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

  const exportAgent = useCallback(
    async (id: string, agentKey: string, include?: string[]) => {
      try {
        const params: Record<string, string> = {};
        if (include && include.length > 0) params.include = include.join(",");
        const data = await http.get<Record<string, unknown>>(`/v1/agents/${id}/export`, params);
        const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `${agentKey}.agent.json`;
        a.click();
        URL.revokeObjectURL(url);
        toast.success(i18n.t("agents:toast.exported"));
      } catch (err) {
        toast.error(i18n.t("agents:toast.exportFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http],
  );

  const importAgent = useCallback(
    async (data: Record<string, unknown>, overrides?: { agent_key?: string; display_name?: string }) => {
      try {
        const params: Record<string, string> = {};
        if (overrides?.agent_key) params.agent_key = overrides.agent_key;
        if (overrides?.display_name) params.display_name = overrides.display_name;
        const qs = new URLSearchParams(params).toString();
        const url = `/v1/agents/import${qs ? `?${qs}` : ""}`;
        const res = await http.post<AgentData>(url, data);
        await invalidate();
        toast.success(i18n.t("agents:toast.imported"), `${res.display_name || res.agent_key || "Agent"}`);
        return res;
      } catch (err) {
        toast.error(i18n.t("agents:toast.importFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const mergeImport = useCallback(
    async (agentId: string, data: Record<string, unknown>, include?: string[]) => {
      try {
        const params: Record<string, string> = {};
        if (include && include.length > 0) params.include = include.join(",");
        const qs = new URLSearchParams(params).toString();
        const url = `/v1/agents/${agentId}/import${qs ? `?${qs}` : ""}`;
        const res = await http.post<{ ok: boolean; imported: string[] }>(url, data);
        await invalidate();
        toast.success(i18n.t("agents:toast.mergeImported"), res.imported?.join(", ") || "");
        return res;
      } catch (err) {
        toast.error(i18n.t("agents:toast.mergeImportFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  return { agents, loading, error, refresh: invalidate, createAgent, updateAgent, deleteAgent, resummonAgent, exportAgent, importAgent, mergeImport };
}
