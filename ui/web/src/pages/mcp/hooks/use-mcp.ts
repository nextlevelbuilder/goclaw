import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { MCPServerData, MCPServerInput, MCPAgentGrant } from "@/types/mcp";

export type { MCPServerData, MCPServerInput, MCPAgentGrant };

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
      const res = await http.post<MCPServerData>("/v1/mcp/servers", data);
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const updateServer = useCallback(
    async (id: string, data: Partial<MCPServerInput>) => {
      await http.put(`/v1/mcp/servers/${id}`, data);
      await invalidate();
    },
    [http, invalidate],
  );

  const deleteServer = useCallback(
    async (id: string) => {
      await http.delete(`/v1/mcp/servers/${id}`);
      await invalidate();
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
  };
}
