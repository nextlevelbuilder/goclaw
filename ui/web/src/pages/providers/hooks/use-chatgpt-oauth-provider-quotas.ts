import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@/api/errors";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";

export interface ChatGPTOAuthQuotaWindow {
  label: string;
  used_percent: number;
  remaining_percent: number;
  reset_after_seconds?: number | null;
  reset_at?: string | null;
}

export interface ChatGPTOAuthQuotaCoreUsageWindow {
  label: string;
  remaining_percent: number;
  reset_after_seconds?: number | null;
  reset_at?: string | null;
}

export interface ChatGPTOAuthProviderQuota {
  provider_name: string;
  success: boolean;
  plan_type?: string | null;
  windows: ChatGPTOAuthQuotaWindow[];
  core_usage?: {
    five_hour?: ChatGPTOAuthQuotaCoreUsageWindow | null;
    weekly?: ChatGPTOAuthQuotaCoreUsageWindow | null;
  } | null;
  last_updated: string;
  error?: string;
  error_code?: string;
  action_hint?: string;
  needs_reauth?: boolean;
  is_forbidden?: boolean;
  retryable?: boolean;
}

function failedQuota(providerName: string, error: unknown): ChatGPTOAuthProviderQuota {
  const fallback: ChatGPTOAuthProviderQuota = {
    provider_name: providerName,
    success: false,
    windows: [],
    last_updated: new Date().toISOString(),
    error_code: "quota_request_failed",
    error: "Quota request failed",
  };

  if (error instanceof ApiError) {
    return {
      ...fallback,
      error_code: error.code || fallback.error_code,
      error: error.message || fallback.error,
    };
  }

  if (error instanceof Error) {
    return {
      ...fallback,
      error: error.message || fallback.error,
    };
  }

  return fallback;
}

export function useChatGPTOAuthProviderQuotas(providerNames: string[], enabled = true) {
  const http = useHttp();
  const stableNames = useMemo(
    () => Array.from(new Set(providerNames.filter(Boolean))).sort(),
    [providerNames],
  );

  const query = useQuery({
    queryKey: queryKeys.providers.chatgptOAuthQuotas(stableNames),
    enabled: enabled && stableNames.length > 0,
    staleTime: 15_000,
    queryFn: async () => {
      const results = await Promise.allSettled(
        stableNames.map((providerName) => http.get<ChatGPTOAuthProviderQuota>(
          `/v1/auth/chatgpt/${encodeURIComponent(providerName)}/quota`,
        )),
      );

      return stableNames.map((providerName, index) => {
        const result = results[index];
        if (result?.status === "fulfilled") {
          return {
            ...result.value,
            provider_name: result.value.provider_name || providerName,
          };
        }
        return failedQuota(providerName, result?.reason);
      });
    },
  });

  const quotas = query.data ?? [];
  const quotaByName = useMemo(
    () => new Map(quotas.map((quota) => [quota.provider_name, quota])),
    [quotas],
  );

  return {
    ...query,
    quotas,
    quotaByName,
  };
}
