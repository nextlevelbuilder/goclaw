import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";
import type {
  ChannelCapability,
  ChannelContextData,
  ChannelContextMember,
  ChannelInstanceData,
} from "@/types/channel";
import type { ChannelContact } from "@/types/contact";

export type { ChannelContact };
export type ChannelCapabilityMutation = {
  scopeType: string;
  scopeKey: string;
  capability: ChannelCapability;
};

export interface ChannelCredentialPayload {
  apiKey?: string;
  env?: Record<string, string>;
}

export interface GroupManagerGroupInfo {
  group_id: string;
  writer_count: number;
}

export interface GroupManagerData {
  user_id: string;
  display_name?: string;
  username?: string;
}

export function useChannelDetail(instanceId: string | undefined) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data, isLoading: loading } = useQuery({
    queryKey: queryKeys.channels.detail(instanceId ?? ""),
    queryFn: async () => {
      return http.get<ChannelInstanceData>(`/v1/channels/instances/${instanceId}`);
    },
    staleTime: 60_000,
    enabled: !!instanceId,
  });

  const instance = data ?? null;

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: queryKeys.channels.detail(instanceId ?? "") });
    queryClient.invalidateQueries({ queryKey: queryKeys.channels.all });
  }, [queryClient, instanceId]);

  const updateInstance = useCallback(
    async (updates: Record<string, unknown>) => {
      if (!instanceId) return;
      try {
        await http.put(`/v1/channels/instances/${instanceId}`, updates);
        await invalidate();
        toast.success(i18next.t("channels:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("channels:toast.failedUpdate"), userFriendlyError(err));
        throw err;
      }
    },
    [instanceId, http, invalidate],
  );

  // Managers API (backend routes still use /writers paths)
  const listManagerGroups = useCallback(
    async (): Promise<GroupManagerGroupInfo[]> => {
      if (!instanceId) return [];
      const res = await http.get<{ groups: GroupManagerGroupInfo[] }>(`/v1/channels/instances/${instanceId}/writers/groups`);
      return res.groups ?? [];
    },
    [instanceId, http],
  );

  const listManagers = useCallback(
    async (groupId: string): Promise<GroupManagerData[]> => {
      if (!instanceId) return [];
      const res = await http.get<{ writers: GroupManagerData[] }>(`/v1/channels/instances/${instanceId}/writers`, { group_id: groupId });
      return res.writers ?? [];
    },
    [instanceId, http],
  );

  const addManager = useCallback(
    async (groupId: string, userId: string, displayName?: string, username?: string) => {
      if (!instanceId) return;
      await http.post(`/v1/channels/instances/${instanceId}/writers`, {
        group_id: groupId,
        user_id: userId,
        display_name: displayName ?? "",
        username: username ?? "",
      });
    },
    [instanceId, http],
  );

  const removeManager = useCallback(
    async (groupId: string, userId: string) => {
      if (!instanceId) return;
      await http.delete(`/v1/channels/instances/${instanceId}/writers/${userId}?group_id=${encodeURIComponent(groupId)}`);
    },
    [instanceId, http],
  );

  const listContexts = useCallback(async (): Promise<ChannelContextData[]> => {
    if (!instanceId) return [];
    const res = await http.get<{ contexts: ChannelContextData[] }>(
      `/v1/channels/instances/${instanceId}/contexts`,
    );
    return res.contexts ?? [];
  }, [instanceId, http]);

  const listContextMembers = useCallback(
    async (scopeType: string, scopeKey: string): Promise<ChannelContextMember[]> => {
      if (!instanceId) return [];
      const res = await http.get<{ members: ChannelContextMember[] }>(
        `/v1/channels/instances/${instanceId}/contexts/${encodeURIComponent(scopeType)}/${encodeURIComponent(scopeKey)}/members`,
      );
      return res.members ?? [];
    },
    [instanceId, http],
  );

  const listContextCapabilities = useCallback(
    async (scopeType: string, scopeKey: string): Promise<ChannelCapability[]> => {
      if (!instanceId) return [];
      const res = await http.get<{ capabilities: ChannelCapability[] }>(
        `/v1/channels/instances/${instanceId}/contexts/${encodeURIComponent(scopeType)}/${encodeURIComponent(scopeKey)}/capabilities`,
      );
      return res.capabilities ?? [];
    },
    [instanceId, http],
  );

  const contextPath = useCallback((scopeType: string, scopeKey: string) => {
    return `/v1/channels/instances/${instanceId}/contexts/${encodeURIComponent(scopeType)}/${encodeURIComponent(scopeKey)}`;
  }, [instanceId]);

  const upsertContextGrant = useCallback(
    async ({ scopeType, scopeKey, capability }: ChannelCapabilityMutation) => {
      if (!instanceId) return;
      const segment = capability.type === "mcp_server" ? "mcp-grants" : "cli-grants";
      await http.put(`${contextPath(scopeType, scopeKey)}/${segment}/${capability.id}`, { enabled: true });
      toast.success(i18next.t("channels:detail.contexts.grantSaved"));
    },
    [contextPath, http, instanceId],
  );

  const deleteContextGrant = useCallback(
    async ({ scopeType, scopeKey, capability }: ChannelCapabilityMutation) => {
      if (!instanceId) return;
      const segment = capability.type === "mcp_server" ? "mcp-grants" : "cli-grants";
      await http.delete(`${contextPath(scopeType, scopeKey)}/${segment}/${capability.id}`);
      toast.success(i18next.t("channels:detail.contexts.grantDeleted"));
    },
    [contextPath, http, instanceId],
  );

  const setContextCredentials = useCallback(
    async ({ scopeType, scopeKey, capability }: ChannelCapabilityMutation, payload: ChannelCredentialPayload) => {
      if (!instanceId) return;
      const segment = capability.type === "mcp_server" ? "mcp-credentials" : "cli-credentials";
      const body = capability.type === "mcp_server"
        ? { api_key: payload.apiKey, env: payload.env }
        : { env_vars: payload.env };
      await http.put(`${contextPath(scopeType, scopeKey)}/${segment}/${capability.id}`, body);
      toast.success(i18next.t("channels:detail.contexts.credentialSaved"));
    },
    [contextPath, http, instanceId],
  );

  const deleteContextCredentials = useCallback(
    async ({ scopeType, scopeKey, capability }: ChannelCapabilityMutation) => {
      if (!instanceId) return;
      const segment = capability.type === "mcp_server" ? "mcp-credentials" : "cli-credentials";
      await http.delete(`${contextPath(scopeType, scopeKey)}/${segment}/${capability.id}`);
      toast.success(i18next.t("channels:detail.contexts.credentialDeleted"));
    },
    [contextPath, http, instanceId],
  );

  const listContacts = useCallback(
    async (search: string, channelType?: string): Promise<ChannelContact[]> => {
      const params: Record<string, string> = {};
      if (search) params.search = search;
      if (channelType) params.channel_type = channelType;
      params.limit = "20";
      const res = await http.get<{ contacts: ChannelContact[] }>("/v1/contacts", params);
      return res.contacts ?? [];
    },
    [http],
  );

  return {
    instance,
    loading,
    updateInstance,
    listManagerGroups,
    listManagers,
    addManager,
    removeManager,
    listContexts,
    listContextMembers,
    listContextCapabilities,
    upsertContextGrant,
    deleteContextGrant,
    setContextCredentials,
    deleteContextCredentials,
    listContacts,
    refresh: invalidate,
  };
}
