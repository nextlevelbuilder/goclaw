import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";
import type { ChannelInstanceData } from "@/types/channel";
import type { ChannelContact } from "@/types/contact";

export type { ChannelContact };

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
    listContacts,
    refresh: invalidate,
  };
}
