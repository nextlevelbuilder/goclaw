import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { ChannelInstanceData, ChannelInstanceInput } from "@/types/channel";

export type { ChannelInstanceData, ChannelInstanceInput };

export interface ChannelInstanceFilters {
  search?: string;
  limit?: number;
  offset?: number;
}

export function useChannelInstances(filters: ChannelInstanceFilters = {}) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryKey = queryKeys.channels.list({ ...filters });

  const { data, isLoading: loading } = useQuery({
    queryKey,
    queryFn: async () => {
      const params: Record<string, string> = {};
      if (filters.search) params.search = filters.search;
      if (filters.limit) params.limit = String(filters.limit);
      if (filters.offset !== undefined) params.offset = String(filters.offset);

      const res = await http.get<{ instances: ChannelInstanceData[]; total?: number }>("/v1/channels/instances", params);
      return { instances: res.instances ?? [], total: res.total ?? 0 };
    },
    placeholderData: (prev) => prev,
    staleTime: 60_000,
  });

  const instances = data?.instances ?? [];
  const total = data?.total ?? 0;

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.channels.all }),
    [queryClient],
  );

  const createInstance = useCallback(
    async (data: ChannelInstanceInput) => {
      try {
        const res = await http.post<{ id: string }>("/v1/channels/instances", data);
        await invalidate();
        toast.success(
          i18next.t("channels:toast.created"),
          i18next.t("channels:toast.createdDesc", { name: data.name }),
        );
        return res;
      } catch (err) {
        toast.error(i18next.t("channels:toast.failedCreate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const updateInstance = useCallback(
    async (id: string, data: Partial<ChannelInstanceInput>) => {
      try {
        await http.put(`/v1/channels/instances/${id}`, data);
        await invalidate();
        toast.success(i18next.t("channels:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("channels:toast.failedUpdate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteInstance = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/channels/instances/${id}`);
        await invalidate();
        toast.success(i18next.t("channels:toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("channels:toast.failedDelete"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  return { instances, total, loading, refresh: invalidate, createInstance, updateInstance, deleteInstance };
}
