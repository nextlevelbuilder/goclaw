import { useCallback } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { userFriendlyError } from "@/lib/error-utils";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type {
  ChannelMemoryConfig,
  ChannelMemoryExtractionItem,
  ChannelMemoryExtractionRun,
  ChannelMemoryStatus,
} from "@/types/channel";

const defaultParams = {};

export function useChannelMemoryExtraction(instanceId: string | undefined) {
  const http = useHttp();
  const queryClient = useQueryClient();
  const statusKey = queryKeys.channels.memoryExtraction(instanceId ?? "");

  const invalidate = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: statusKey }),
      queryClient.invalidateQueries({
        queryKey: queryKeys.channels.memoryExtractionItems(instanceId ?? "", defaultParams),
      }),
      queryClient.invalidateQueries({ queryKey: queryKeys.channels.detail(instanceId ?? "") }),
    ]);
  }, [instanceId, queryClient, statusKey]);

  const statusQuery = useQuery({
    queryKey: statusKey,
    queryFn: () => http.get<ChannelMemoryStatus>(`/v1/channels/instances/${instanceId}/memory-extraction`),
    enabled: !!instanceId,
    staleTime: 30_000,
  });

  const itemsQuery = useQuery({
    queryKey: queryKeys.channels.memoryExtractionItems(instanceId ?? "", defaultParams),
    queryFn: async () => {
      const res = await http.get<{ items: ChannelMemoryExtractionItem[] }>(
        `/v1/channels/instances/${instanceId}/memory-extraction/items`,
      );
      return res.items ?? [];
    },
    enabled: !!instanceId,
    staleTime: 30_000,
  });

  const saveSettings = useMutation({
    mutationFn: (config: ChannelMemoryConfig) => {
      return http.put<{ config: ChannelMemoryConfig }>(
        `/v1/channels/instances/${instanceId}/memory-extraction/settings`,
        config,
      );
    },
    onSuccess: async () => {
      await invalidate();
      toast.success(i18next.t("channels:detail.passiveMemory.saved"));
    },
    onError: (err) => {
      toast.error(i18next.t("channels:detail.passiveMemory.saveFailed"), userFriendlyError(err));
    },
  });

  const runNow = useMutation({
    mutationFn: () => http.post<{ run: ChannelMemoryExtractionRun }>(
      `/v1/channels/instances/${instanceId}/memory-extraction/run`,
    ),
    onSuccess: async () => {
      await invalidate();
      toast.success(i18next.t("channels:detail.passiveMemory.runQueued"));
    },
    onError: (err) => {
      toast.error(i18next.t("channels:detail.passiveMemory.runFailed"), userFriendlyError(err));
    },
  });

  const itemAction = useMutation({
    mutationFn: async ({ id, action }: { id: string; action: "approve" | "reject" | "delete" }) => {
      const base = `/v1/channels/instances/${instanceId}/memory-extraction/items/${id}`;
      if (action === "delete") {
        return http.delete(base);
      }
      return http.post(`${base}/${action}`);
    },
    onSuccess: async () => {
      await invalidate();
      toast.success(i18next.t("channels:detail.passiveMemory.itemUpdated"));
    },
    onError: (err) => {
      toast.error(i18next.t("channels:detail.passiveMemory.itemFailed"), userFriendlyError(err));
    },
  });

  return {
    status: statusQuery.data ?? null,
    items: itemsQuery.data ?? statusQuery.data?.recent_items ?? [],
    loading: statusQuery.isLoading || itemsQuery.isLoading,
    saveSettings,
    runNow,
    itemAction,
  };
}
