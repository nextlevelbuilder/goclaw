import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ModelInfo, ProviderModelsResponse } from "@/types/provider";

export type { ModelInfo };

export function useProviderModels(providerId: string | undefined) {
  const http = useHttp();

  const {
    data,
    isLoading: loading,
  } = useQuery({
    queryKey: queryKeys.providers.models(providerId ?? ""),
    queryFn: async () => {
      const res = await http.get<ProviderModelsResponse>(
        `/v1/providers/${providerId}/models`,
      );
      return res;
    },
    enabled: !!providerId,
  });

  return {
    models: data?.models ?? [],
    reasoningDefaults: data?.reasoning_defaults ?? null,
    loading,
  };
}
