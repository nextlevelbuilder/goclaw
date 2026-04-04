import { useState, useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ComboboxOption } from "@/components/ui/combobox";

/** Unified search result from contacts + tenant_users. */
export interface UserPickerItem {
  id: string;
  display_name?: string;
  username?: string;
  source: "contact" | "tenant_user";
  channel_type?: string;
  peer_kind?: string;
  merged_tenant_user_id?: string;
  role?: string;
}

/**
 * Unified user picker hook — searches both channel_contacts and tenant_users.
 * - Empty search: returns 30 most recent
 * - With search: debounced server-side ILIKE search
 * - Deduplicates merged contacts (shows tenant_user badge instead)
 */
/**
 * @param source - Filter by source: "contact" | "tenant_user" | undefined (both).
 *   Use "tenant_user" for merge dialog / add tenant user (contacts excluded).
 */
export function useUserPicker(search: string, peerKind?: string, source?: "contact" | "tenant_user") {
  const http = useHttp();
  const [debouncedSearch, setDebouncedSearch] = useState("");

  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(search), 150);
    return () => clearTimeout(timer);
  }, [search]);

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.users.search({ search: debouncedSearch, peerKind, source, limit: 30 }),
    queryFn: async () => {
      const params: Record<string, string> = { limit: "30" };
      if (debouncedSearch) params.q = debouncedSearch;
      if (peerKind) params.peer_kind = peerKind;
      if (source) params.source = source;
      const res = await http.get<{ results: UserPickerItem[] }>("/v1/users/search", params);
      return res.results ?? [];
    },
    // Always enabled — empty search returns recent contacts
    staleTime: 30_000,
  });

  const results = data ?? [];

  /** Format results as ComboboxOptions with source badges. */
  const options: ComboboxOption[] = useMemo(() =>
    results.map((r) => {
      const parts: string[] = [];
      if (r.display_name) parts.push(r.display_name);
      if (r.username) parts.push(`@${r.username}`);
      parts.push(`(${r.id})`);
      if (r.source === "contact" && r.channel_type) parts.push(`[${r.channel_type}]`);
      if (r.source === "tenant_user") parts.push("[tenant]");
      if (r.merged_tenant_user_id) parts.push(`→ ${r.merged_tenant_user_id}`);
      return { value: r.id, label: parts.join(" ") };
    }),
    [results],
  );

  return { results, options, loading: isLoading };
}
