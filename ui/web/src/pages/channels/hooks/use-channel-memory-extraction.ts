import { useCallback, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { userFriendlyError } from "@/lib/error-utils";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type {
  ChannelMemoryConfig,
  ChannelMemoryGroupOption,
  ChannelMemoryItemsResponse,
  ChannelMemoryProcessAllEvent,
  ChannelMemoryProcessAllResult,
  ChannelMemoryStatus,
} from "@/types/channel";

interface ChannelMemoryExtractionParams {
  page: number;
  pageSize: number;
  loadGroupOptions?: boolean;
}

export function useChannelMemoryExtraction(instanceId: string | undefined, params: ChannelMemoryExtractionParams) {
  const http = useHttp();
  const queryClient = useQueryClient();
  const statusKey = queryKeys.channels.memoryExtraction(instanceId ?? "");
  const [runAllEvents, setRunAllEvents] = useState<ChannelMemoryProcessAllEvent[]>([]);

  const itemParams = useMemo(() => ({
    limit: String(params.pageSize),
    offset: String((params.page - 1) * params.pageSize),
  }), [params.page, params.pageSize]);

  const itemsRootKey = useMemo(
    () => ["channels", "detail", instanceId ?? "", "memory-extraction", "items"] as const,
    [instanceId],
  );

  const invalidate = useCallback(async (options?: { includeGroups?: boolean }) => {
    const queries = [
      queryClient.invalidateQueries({ queryKey: statusKey, exact: true }),
      queryClient.invalidateQueries({ queryKey: itemsRootKey }),
      queryClient.invalidateQueries({ queryKey: queryKeys.channels.detail(instanceId ?? ""), exact: true }),
    ];
    if (options?.includeGroups) {
      queries.push(queryClient.invalidateQueries({ queryKey: queryKeys.channels.memoryExtractionGroups(instanceId ?? ""), exact: true }));
    }
    await Promise.all(queries);
  }, [instanceId, itemsRootKey, queryClient, statusKey]);

  const statusQuery = useQuery({
    queryKey: statusKey,
    queryFn: () => http.get<ChannelMemoryStatus>(`/v1/channels/instances/${instanceId}/memory-extraction`),
    enabled: !!instanceId,
    staleTime: 30_000,
  });

  const itemsQuery = useQuery({
    queryKey: queryKeys.channels.memoryExtractionItems(instanceId ?? "", itemParams),
    queryFn: async () => {
      const res = await http.get<ChannelMemoryItemsResponse>(
        `/v1/channels/instances/${instanceId}/memory-extraction/items`,
        itemParams,
      );
      return res;
    },
    enabled: !!instanceId,
    staleTime: 30_000,
  });

  const groupsQuery = useQuery({
    queryKey: queryKeys.channels.memoryExtractionGroups(instanceId ?? ""),
    queryFn: async () => {
      const res = await http.get<{ groups: ChannelMemoryGroupOption[] }>(
        `/v1/channels/instances/${instanceId}/memory-extraction/groups`,
      );
      return res.groups ?? [];
    },
    enabled: !!instanceId && params.loadGroupOptions === true,
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
      await invalidate({ includeGroups: true });
      toast.success(i18next.t("channels:detail.passiveMemory.saved"));
    },
    onError: (err) => {
      toast.error(i18next.t("channels:detail.passiveMemory.saveFailed"), userFriendlyError(err));
    },
  });

  const runNow = useMutation({
    mutationFn: () => http.post(
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

  const runAll = useMutation({
    mutationFn: () => runAllGroupsStream(http, instanceId, (event) => {
      setRunAllEvents((events) => [...events, event].slice(-12));
    }),
    onMutate: () => {
      setRunAllEvents([]);
    },
    onSuccess: async (result) => {
      await invalidate();
      toast.success(
        i18next.t("channels:detail.passiveMemory.runAllDone", {
          messages: result.message_count,
          runs: result.run_count,
        }),
      );
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
    items: itemsQuery.data?.items ?? statusQuery.data?.recent_items ?? [],
    itemsTotal: itemsQuery.data?.total ?? statusQuery.data?.recent_items?.length ?? 0,
    groupOptions: groupsQuery.data ?? [],
    loading: statusQuery.isLoading || itemsQuery.isLoading,
    saveSettings,
    runNow,
    runAll,
    runAllEvents,
    itemAction,
  };
}

async function runAllGroupsStream(
  http: ReturnType<typeof useHttp>,
  instanceId: string | undefined,
  onEvent: (event: ChannelMemoryProcessAllEvent) => void,
): Promise<ChannelMemoryProcessAllResult> {
  const res = await fetch(http.rawUrl(`/v1/channels/instances/${instanceId}/memory-extraction/run-all`), {
    method: "POST",
    headers: {
      ...http.getAuthHeaders(),
      Accept: "application/x-ndjson",
    },
  });
  if (!res.ok) {
    throw new Error(res.statusText);
  }
  if (!res.body) {
    throw new Error("Streaming response is empty");
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let final: ChannelMemoryProcessAllEvent | null = null;

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? "";
    for (const line of lines) {
      const event = parseRunAllEvent(line);
      if (!event) continue;
      if (event.type === "error") {
        throw new Error(event.error || "Memory extraction failed");
      }
      final = event.type === "final" ? event : final;
      onEvent(event);
    }
  }

  const tail = parseRunAllEvent(buffer);
  if (tail) {
    if (tail.type === "error") {
      throw new Error(tail.error || "Memory extraction failed");
    }
    final = tail.type === "final" ? tail : final;
    onEvent(tail);
  }

  return {
    runs: [],
    run_count: final?.run_count ?? 0,
    message_count: final?.message_count ?? 0,
    item_count: final?.item_count ?? 0,
    skipped_group_count: final?.skipped_group_count ?? 0,
    error_count: final?.error_count ?? 0,
  };
}

function parseRunAllEvent(line: string): ChannelMemoryProcessAllEvent | null {
  const trimmed = line.trim();
  if (!trimmed) return null;
  return JSON.parse(trimmed) as ChannelMemoryProcessAllEvent;
}
