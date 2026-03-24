import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ModelInfo } from "@/types/provider";

export type { ModelInfo };

// Hardcoded models for the OpenAI Codex OAuth provider.
// OAuth token lacks api.model.read scope, so we can't call /v1/models.
const CODEX_MODELS: ModelInfo[] = [
  { id: "gpt-5.4", name: "GPT-5.4" },
  { id: "gpt-5.4-mini", name: "GPT-5.4 Mini" },
  { id: "gpt-5.3-codex", name: "GPT-5.3 Codex" },
  { id: "gpt-5.3-codex-spark", name: "GPT-5.3 Codex Spark" },
  { id: "gpt-5.2-codex", name: "GPT-5.2 Codex" },
  { id: "gpt-5.2", name: "GPT-5.2" },
  { id: "gpt-5.1-codex", name: "GPT-5.1 Codex" },
  { id: "gpt-5.1-codex-max", name: "GPT-5.1 Codex Max" },
  { id: "gpt-5.1-codex-mini", name: "GPT-5.1 Codex Mini" },
  { id: "gpt-5.1", name: "GPT-5.1" },
];

export function useProviderModels(providerId: string | undefined, providerType?: string) {
  const http = useHttp();
  const isOAuth = providerType === "chatgpt_oauth";

  const { data: models = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.providers.models(providerId ?? ""),
    queryFn: async () => {
      if (isOAuth) return CODEX_MODELS;
      const res = await http.get<{ models: ModelInfo[] }>(
        `/v1/providers/${providerId}/models`,
      );
      return res.models ?? [];
    },
    enabled: !!providerId,
  });

  return { models, loading };
}
