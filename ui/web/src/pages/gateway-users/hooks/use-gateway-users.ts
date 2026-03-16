import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { GatewayUserData, GatewayUserCreateInput, GatewayUserCreateResponse } from "@/types/gateway-user";

export function useGatewayUsers() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: users = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.gatewayUsers.all,
    queryFn: () => http.get<GatewayUserData[]>("/v1/gateway-users"),
    staleTime: 60_000,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.gatewayUsers.all }),
    [queryClient],
  );

  const createUser = useCallback(
    async (data: GatewayUserCreateInput): Promise<GatewayUserCreateResponse> => {
      try {
        const res = await http.post<GatewayUserCreateResponse>("/v1/gateway-users", data);
        await invalidate();
        toast.success(i18next.t("gateway-users:toast.created"));
        return res;
      } catch (err) {
        toast.error(i18next.t("gateway-users:toast.failedCreate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteUser = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/gateway-users/${id}`);
        await invalidate();
        toast.success(i18next.t("gateway-users:toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("gateway-users:toast.failedDelete"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  return { users, loading, refresh: invalidate, createUser, deleteUser };
}
