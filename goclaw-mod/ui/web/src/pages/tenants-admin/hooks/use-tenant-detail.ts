import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useWs } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import { Methods } from "@/api/protocol";
import type { TenantData, TenantUserData } from "@/types/tenant";

export function useTenantDetail(tenantId: string) {
  const ws = useWs();
  const queryClient = useQueryClient();

  const { data: tenant, isLoading: tenantLoading } = useQuery({
    queryKey: queryKeys.tenants.detail(tenantId),
    queryFn: async () => {
      const res = await ws.call<TenantData>(Methods.TENANTS_GET, { id: tenantId });
      return res ?? null;
    },
    enabled: !!tenantId,
    staleTime: 60_000,
  });

  const { data: users = [], isLoading: usersLoading, isFetching: usersRefreshing } = useQuery({
    queryKey: queryKeys.tenants.users(tenantId),
    queryFn: async () => {
      const res = await ws.call<{ users: TenantUserData[] }>(Methods.TENANTS_USERS_LIST, { tenant_id: tenantId });
      return res?.users ?? [];
    },
    enabled: !!tenantId,
    staleTime: 60_000,
  });

  const invalidateUsers = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.tenants.users(tenantId) }),
    [queryClient, tenantId],
  );

  const addUser = useCallback(
    async (userId: string, role: string) => {
      try {
        await ws.call(Methods.TENANTS_USERS_ADD, { tenant_id: tenantId, user_id: userId, role });
        await invalidateUsers();
        toast.success(i18next.t("tenants:addUser"), userId);
      } catch (err) {
        toast.error(i18next.t("tenants:addUser"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws, tenantId, invalidateUsers],
  );

  const removeUser = useCallback(
    async (userId: string) => {
      try {
        await ws.call(Methods.TENANTS_USERS_REMOVE, { tenant_id: tenantId, user_id: userId });
        await invalidateUsers();
        toast.success(i18next.t("tenants:removeUser"), userId);
      } catch (err) {
        toast.error(i18next.t("tenants:removeUser"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws, tenantId, invalidateUsers],
  );

  return {
    tenant,
    tenantLoading,
    users,
    usersLoading,
    usersRefreshing,
    refreshUsers: invalidateUsers,
    addUser,
    removeUser,
  };
}
