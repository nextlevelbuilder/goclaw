import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ProviderData } from "@/types/provider";

interface ChatGPTOAuthStatusResponse {
  authenticated: boolean;
}

export type ChatGPTOAuthAvailability = "ready" | "needs_sign_in" | "disabled";

export interface ChatGPTOAuthProviderStatus {
  provider: ProviderData;
  authenticated: boolean;
  availability: ChatGPTOAuthAvailability;
}

export function useChatGPTOAuthProviderStatuses(providers: ProviderData[], enabled = true) {
  const http = useHttp();
  const oauthProviders = useMemo(
    () => providers.filter((provider) => provider.provider_type === "chatgpt_oauth"),
    [providers],
  );
  const providerKeys = oauthProviders.map((provider) => `${provider.name}:${provider.enabled ? "1" : "0"}`);

  const query = useQuery({
    queryKey: queryKeys.providers.chatgptOAuthStatuses(providerKeys),
    enabled: enabled && oauthProviders.length > 0,
    staleTime: 10_000,
    queryFn: async () => Promise.all(
      oauthProviders.map(async (provider) => {
        if (!provider.enabled) {
          return {
            provider,
            authenticated: false,
            availability: "disabled" as const,
          };
        }

        try {
          const status = await http.get<ChatGPTOAuthStatusResponse>(
            `/v1/auth/chatgpt/${encodeURIComponent(provider.name)}/status`,
          );
          return {
            provider,
            authenticated: Boolean(status.authenticated),
            availability: status.authenticated ? "ready" as const : "needs_sign_in" as const,
          };
        } catch {
          return {
            provider,
            authenticated: false,
            availability: "needs_sign_in" as const,
          };
        }
      }),
    ),
  });

  return {
    ...query,
    statuses: query.data ?? [],
  };
}
