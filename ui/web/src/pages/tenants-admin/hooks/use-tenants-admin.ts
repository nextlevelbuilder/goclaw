import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useWs } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import { Methods } from "@/api/protocol";
import type { TenantData, TenantMembership } from "@/types/tenant";

interface TenantsResult {
  tenants: TenantData[];
  isOwner: boolean;
}

export function useTenantsAdmin() {
  const ws = useWs();
  const queryClient = useQueryClient();

  const { data, isLoading: loading, isFetching: refreshing } = useQuery({
    queryKey: queryKeys.tenants.all,
    queryFn: async (): Promise<TenantsResult> => {
      try {
        // Owner/system: full tenant list
        const res = await ws.call<{ tenants: TenantData[] }>(Methods.TENANTS_LIST);
        return { tenants: res?.tenants ?? [], isOwner: true };
      } catch {
        // Non-owner: fall back to user's own tenants
        const res = await ws.call<{ tenants: TenantMembership[] }>(Methods.TENANTS_MINE);
        const tenants: TenantData[] = (res?.tenants ?? []).map((m) => ({
          id: m.id,
          name: m.name,
          slug: m.slug,
          status: m.status,
          created_at: "",
          updated_at: "",
        }));
        return { tenants, isOwner: false };
      }
    },
    staleTime: 30_000,
  });

  const tenants = data?.tenants ?? [];
  const isOwner = data?.isOwner ?? false;

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.tenants.all }),
    [queryClient],
  );

  const createTenant = useCallback(
    async (data: { name: string; slug: string }) => {
      try {
        const res = await ws.call<TenantData>(Methods.TENANTS_CREATE, data);
        await invalidate();
        toast.success(i18next.t("tenants:createTenant"), i18next.t("tenants:name") + ": " + data.name);
        return res;
      } catch (err) {
        toast.error(i18next.t("tenants:createTenant"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws, invalidate],
  );

  return { tenants, loading, refreshing, refresh: invalidate, createTenant, isOwner };
}
