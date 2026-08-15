import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs } from "@/hooks/use-ws";
import type { BitrixPortal, BitrixPortalCreateInput, BitrixPortalCreateResult } from "./types";

// Single shared cache key so list, create, delete, and authorize-poll all
// invalidate/read the same data. Adding nesting (e.g. ["bitrix", "portals", "list", tenantId])
// is unnecessary — the backend scopes by tenant via WS connection auth.
const PORTALS_QUERY_KEY = ["bitrix", "portals", "list"] as const;

interface UseBitrixPortalsOptions {
  /** When set, react-query polls the list at this interval (ms). Used by the
   *  authorize step to detect install completion without manual refresh. */
  pollInterval?: number;
  /** Disable the query entirely (e.g. when modal is closed). */
  enabled?: boolean;
}

export function useBitrixPortals(opts: UseBitrixPortalsOptions = {}) {
  const ws = useWs();
  return useQuery({
    queryKey: PORTALS_QUERY_KEY,
    queryFn: async () => {
      const res = await ws.call<{ portals: BitrixPortal[] }>("bitrix.portals.list", {});
      return res.portals ?? [];
    },
    staleTime: 60_000,
    refetchInterval: opts.pollInterval,
    enabled: opts.enabled ?? true,
  });
}

export function useBitrixPortalCreate() {
  const ws = useWs();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: BitrixPortalCreateInput) =>
      ws.call<BitrixPortalCreateResult>("bitrix.portals.create", { ...input }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bitrix", "portals"] });
    },
  });
}

export function useBitrixPortalGetInstallURL() {
  const ws = useWs();
  return useMutation({
    mutationFn: (name: string) =>
      ws.call<{ install_url: string }>("bitrix.portals.get_install_url", { name }),
  });
}

export function useBitrixPortalDelete() {
  const ws = useWs();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      ws.call<{ status: string }>("bitrix.portals.delete", { name }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bitrix", "portals"] });
    },
  });
}
