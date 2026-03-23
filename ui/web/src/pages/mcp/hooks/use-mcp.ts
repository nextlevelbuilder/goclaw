import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { MCPServerData, MCPServerInput, MCPAgentGrant, MCPToolInfo, MCPUserCredentialStatus, MCPUserCredentialInput } from "@/types/mcp";

export type { MCPServerData, MCPServerInput, MCPAgentGrant, MCPToolInfo, MCPUserCredentialStatus, MCPUserCredentialInput };

export function useMCP() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: servers = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.mcp.all,
    queryFn: async () => {
      const res = await http.get<{ servers: MCPServerData[] }>("/v1/mcp/servers");
      return res.servers ?? [];
    },
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.mcp.all }),
    [queryClient],
  );

  const createServer = useCallback(
    async (data: MCPServerInput) => {
      try {
        const res = await http.post<MCPServerData>("/v1/mcp/servers", data);
        await invalidate();
        toast.success(i18next.t("mcp:toast.created"));
        return res;
      } catch (err) {
        toast.error(i18next.t("mcp:toast.failedCreate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const updateServer = useCallback(
    async (id: string, data: Partial<MCPServerInput>) => {
      try {
        await http.put(`/v1/mcp/servers/${id}`, data);
        await invalidate();
        toast.success(i18next.t("mcp:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("mcp:toast.failedUpdate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteServer = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/mcp/servers/${id}`);
        await invalidate();
        toast.success(i18next.t("mcp:toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("mcp:toast.failedDelete"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const listAgentGrants = useCallback(
    async (serverId: string) => {
      const res = await http.get<{ grants: MCPAgentGrant[] }>(`/v1/mcp/servers/${serverId}/grants`);
      return res.grants ?? [];
    },
    [http],
  );

  const grantAgent = useCallback(
    async (serverId: string, agentId: string, toolAllow?: string[], toolDeny?: string[]) => {
      await http.post(`/v1/mcp/servers/${serverId}/grants/agent`, {
        agent_id: agentId,
        tool_allow: toolAllow,
        tool_deny: toolDeny,
      });
    },
    [http],
  );

  const revokeAgent = useCallback(
    async (serverId: string, agentId: string) => {
      await http.delete(`/v1/mcp/servers/${serverId}/grants/agent/${agentId}`);
    },
    [http],
  );

  const listGrantsByAgent = useCallback(
    async (agentId: string) => {
      const res = await http.get<{ grants: MCPAgentGrant[] }>(`/v1/mcp/grants/agent/${agentId}`);
      return res.grants ?? [];
    },
    [http],
  );

  const testConnection = useCallback(
    async (data: { transport: string; command?: string; args?: string[]; url?: string; headers?: Record<string, string>; env?: Record<string, string> }) => {
      return http.post<{ success: boolean; tool_count?: number; error?: string }>("/v1/mcp/servers/test", data);
    },
    [http],
  );

  const listServerTools = useCallback(
    async (serverId: string) => {
      const res = await http.get<{ tools: MCPToolInfo[] }>(`/v1/mcp/servers/${serverId}/tools`);
      return res.tools ?? [];
    },
    [http],
  );

  const getUserCredentials = useCallback(
    async (serverId: string) => {
      return http.get<MCPUserCredentialStatus>(`/v1/mcp/servers/${serverId}/user-credentials`);
    },
    [http],
  );

  const setUserCredentials = useCallback(
    async (serverId: string, creds: MCPUserCredentialInput) => {
      await http.put(`/v1/mcp/servers/${serverId}/user-credentials`, creds);
    },
    [http],
  );

  const deleteUserCredentials = useCallback(
    async (serverId: string) => {
      await http.delete(`/v1/mcp/servers/${serverId}/user-credentials`);
    },
    [http],
  );

  return {
    servers,
    loading,
    refresh: invalidate,
    createServer,
    updateServer,
    deleteServer,
    listAgentGrants,
    grantAgent,
    revokeAgent,
    listGrantsByAgent,
    testConnection,
    listServerTools,
    getUserCredentials,
    setUserCredentials,
    deleteUserCredentials,
  };
}
