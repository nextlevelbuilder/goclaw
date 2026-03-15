import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { ApiKeyData, ApiKeyCreateInput, ApiKeyCreateResponse } from "@/types/api-key";

export function useApiKeys() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: apiKeys = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: () => http.get<ApiKeyData[]>("/v1/api-keys"),
    staleTime: 60_000,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.apiKeys.all }),
    [queryClient],
  );

  const createApiKey = useCallback(
    async (data: ApiKeyCreateInput): Promise<ApiKeyCreateResponse> => {
      try {
        const res = await http.post<ApiKeyCreateResponse>("/v1/api-keys", data);
        await invalidate();
        toast.success(i18next.t("api-keys:toast.created"));
        return res;
      } catch (err) {
        toast.error(i18next.t("api-keys:toast.failedCreate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const revokeApiKey = useCallback(
    async (id: string) => {
      try {
        await http.post(`/v1/api-keys/${id}/revoke`, {});
        await invalidate();
        toast.success(i18next.t("api-keys:toast.revoked"));
      } catch (err) {
        toast.error(i18next.t("api-keys:toast.failedRevoke"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  return { apiKeys, loading, refresh: invalidate, createApiKey, revokeApiKey };
}
