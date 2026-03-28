import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";

export interface ChannelGroup {
  id: string;
  channel_type: string;
  channel_instance?: string;
  group_id: string;
  group_name?: string;
  avatar_url?: string;
  member_count: number;
  first_seen_at: string;
  last_seen_at: string;
}

export function useGroups(channelType?: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryKey = queryKeys.groups.list(channelType);

  const { data, isLoading: loading, isFetching } = useQuery({
    queryKey,
    queryFn: async () => {
      const params: Record<string, string> = {};
      if (channelType) params.channel_type = channelType;
      const res = await http.get<{ groups: ChannelGroup[] }>("/v1/groups", params);
      return res.groups ?? [];
    },
    placeholderData: (prev) => prev,
  });

  const groups = data ?? [];

  const refresh = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.groups.all }),
    [queryClient],
  );

  return { groups, loading, fetching: isFetching, refresh };
}
