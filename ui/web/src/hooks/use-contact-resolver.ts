import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ChannelContact } from "@/types/contact";

/**
 * Batch-resolves sender IDs to contact info via GET /v1/contacts/resolve.
 * Results are cached in React Query. Returns a resolve() function for lookups.
 */
export function useContactResolver(senderIDs: string[]) {
  const http = useHttp();

  // Deduplicate and filter empty strings
  const uniqueIDs = [...new Set(senderIDs.filter(Boolean))];
  const idsKey = uniqueIDs.sort().join(",");

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.contacts.resolve(idsKey),
    queryFn: async () => {
      if (uniqueIDs.length === 0) return {};
      const res = await http.get<{ contacts: Record<string, ChannelContact> }>(
        "/v1/contacts/resolve",
        { ids: uniqueIDs.join(",") },
      );
      return res.contacts ?? {};
    },
    enabled: uniqueIDs.length > 0,
    staleTime: 5 * 60 * 1000, // 5 min
  });

  const contactMap = data ?? {};

  /** Resolve a sender_id to its contact info, or null if not found. */
  const resolve = (id: string): ChannelContact | null => {
    return contactMap[id] ?? null;
  };

  return { resolve, loading: isLoading, contactMap };
}
