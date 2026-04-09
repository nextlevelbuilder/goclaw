import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ChannelContact } from "@/types/contact";

export type { ChannelContact };

export interface ContactFilters {
  search?: string;
  channelType?: string;
  peerKind?: string;
  contactType?: string;
  limit?: number;
  offset?: number;
}

export function useContacts(filters: ContactFilters = {}) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryKey = queryKeys.contacts.list({ ...filters });

  const { data, isLoading: loading, isFetching } = useQuery({
    queryKey,
    queryFn: async () => {
      const params: Record<string, string> = {};
      if (filters.search) params.search = filters.search;
      if (filters.channelType) params.channel_type = filters.channelType;
      if (filters.peerKind) params.peer_kind = filters.peerKind;
      if (filters.contactType) params.contact_type = filters.contactType;
      if (filters.limit) params.limit = String(filters.limit);
      if (filters.offset !== undefined) params.offset = String(filters.offset);

      const res = await http.get<{ contacts: ChannelContact[]; total?: number }>("/v1/contacts", params);
      return { contacts: res.contacts ?? [], total: res.total ?? 0 };
    },
    placeholderData: (prev) => prev,
    staleTime: 60_000,
  });

  const contacts = data?.contacts ?? [];
  const total = data?.total ?? 0;

  const refresh = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.contacts.all }),
    [queryClient],
  );

  return { contacts, total, loading, fetching: isFetching, refresh };
}
