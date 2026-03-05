import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { ProviderData, ProviderInput } from "@/types/provider";

export type { ProviderData, ProviderInput };

export function useProviders() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: providers = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.providers.all,
    queryFn: async () => {
      const res = await http.get<{ providers: ProviderData[] }>("/v1/providers");
      return res.providers ?? [];
    },
    staleTime: 60_000,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.providers.all }),
    [queryClient],
  );

  const createProvider = useCallback(
    async (data: ProviderInput) => {
      try {
        const res = await http.post<ProviderData>("/v1/providers", data);
        await invalidate();
        toast.success("Provider created", `${data.name} has been added`);
        return res;
      } catch (err) {
        toast.error("Failed to create provider", err instanceof Error ? err.message : "Unknown error");
        throw err;
      }
    },
    [http, invalidate],
  );

  const updateProvider = useCallback(
    async (id: string, data: Partial<ProviderInput>) => {
      try {
        await http.put(`/v1/providers/${id}`, data);
        await invalidate();
        toast.success("Provider updated");
      } catch (err) {
        toast.error("Failed to update provider", err instanceof Error ? err.message : "Unknown error");
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteProvider = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/providers/${id}`);
        await invalidate();
        toast.success("Provider deleted");
      } catch (err) {
        toast.error("Failed to delete provider", err instanceof Error ? err.message : "Unknown error");
        throw err;
      }
    },
    [http, invalidate],
  );

  return {
    providers,
    loading,
    refresh: invalidate,
    createProvider,
    updateProvider,
    deleteProvider,
  };
}
