import { useState, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ChannelContact } from "@/types/contact";

/**
 * Searches contacts by name/username/sender_id with server-side filtering.
 * Returns results as ComboboxOption-compatible items.
 */
export function useContactSearch(search: string) {
  const http = useHttp();
  const [debouncedSearch, setDebouncedSearch] = useState("");

  // Simple debounce via timeout
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(search), 150);
    return () => clearTimeout(timer);
  }, [search]);

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.contacts.search({ search: debouncedSearch, limit: 20 }),
    queryFn: async () => {
      const params: Record<string, string> = { limit: "20" };
      if (debouncedSearch) params.search = debouncedSearch;
      const res = await http.get<{ contacts: ChannelContact[] }>("/v1/contacts", params);
      return res.contacts ?? [];
    },
    enabled: debouncedSearch.length >= 2,
    staleTime: 30_000,
  });

  return { contacts: data ?? [], loading: isLoading };
}
