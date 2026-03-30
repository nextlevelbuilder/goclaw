import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ModelInfo } from "@/types/provider";

export type { ModelInfo };

export function useProviderModels(providerId: string | undefined, _providerType?: string) {
  const http = useHttp();

  const { data: models = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.providers.models(providerId ?? ""),
    queryFn: async () => {
      const res = await http.get<{ models: ModelInfo[] }>(
        `/v1/providers/${providerId}/models`,
      );
      return res.models ?? [];
    },
    enabled: !!providerId,
  });

  return { models, loading };
}
