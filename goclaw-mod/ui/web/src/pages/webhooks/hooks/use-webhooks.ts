import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type {
  WebhookData,
  WebhookCreateInput,
  WebhookCreateResponse,
  WebhookUpdateInput,
  WebhookRotateResponse,
  WebhookCallData,
  WebhookCallDetail,
  WebhookTestInput,
  WebhookTestResult,
  Paginated,
} from "@/types/webhook";

export interface WebhookListParams {
  limit: number;
  offset: number;
  q?: string;
  includeRevoked?: boolean;
}

export function useWebhooks(params: WebhookListParams) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data, isLoading: loading } = useQuery({
    queryKey: queryKeys.webhooks.list(params as unknown as Record<string, unknown>),
    queryFn: () => {
      const q: Record<string, string> = {
        limit: String(params.limit),
        offset: String(params.offset),
      };
      if (params.q) q.q = params.q;
      if (params.includeRevoked) q.include_revoked = "true";
      return http.get<Paginated<WebhookData>>("/v1/webhooks", q);
    },
    placeholderData: (prev) => prev,
    staleTime: 60_000,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.webhooks.all }),
    [queryClient],
  );

  const createWebhook = useCallback(
    async (data: WebhookCreateInput): Promise<WebhookCreateResponse> => {
      try {
        const res = await http.post<WebhookCreateResponse>("/v1/webhooks", data);
        await invalidate();
        toast.success(i18next.t("webhooks:toast.created"));
        return res;
      } catch (err) {
        toast.error(i18next.t("webhooks:toast.failedCreate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const updateWebhook = useCallback(
    async (id: string, data: WebhookUpdateInput): Promise<void> => {
      try {
        await http.patch(`/v1/webhooks/${id}`, data);
        await invalidate();
        toast.success(i18next.t("webhooks:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("webhooks:toast.failedUpdate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const rotateSecret = useCallback(
    async (id: string): Promise<WebhookRotateResponse> => {
      try {
        const res = await http.post<WebhookRotateResponse>(`/v1/webhooks/${id}/rotate`, {});
        await invalidate();
        toast.success(i18next.t("webhooks:toast.secretRotated"));
        return res;
      } catch (err) {
        toast.error(i18next.t("webhooks:toast.failedRotate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const revokeWebhook = useCallback(
    async (id: string): Promise<void> => {
      try {
        await http.delete(`/v1/webhooks/${id}`);
        await invalidate();
        toast.success(i18next.t("webhooks:toast.revoked"));
      } catch (err) {
        toast.error(i18next.t("webhooks:toast.failedRevoke"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const testWebhook = useCallback(
    (id: string, body: WebhookTestInput): Promise<WebhookTestResult> =>
      http.post<WebhookTestResult>(`/v1/webhooks/${id}/test`, body),
    [http],
  );

  return {
    webhooks: data?.items ?? [],
    total: data?.total ?? 0,
    loading,
    refresh: invalidate,
    createWebhook,
    updateWebhook,
    rotateSecret,
    revokeWebhook,
    testWebhook,
  };
}

export function useWebhookCalls(
  id: string | null,
  status: string,
  enabled: boolean,
  limit: number,
  offset: number,
) {
  const http = useHttp();
  const params = { status, limit, offset };

  const { data, isLoading: loading, isFetching, refetch } = useQuery({
    queryKey: queryKeys.webhooks.calls(id ?? "", params),
    queryFn: () => {
      const q: Record<string, string> = { limit: String(limit), offset: String(offset) };
      if (status) q.status = status;
      return http.get<Paginated<WebhookCallData>>(`/v1/webhooks/${id}/calls`, q);
    },
    enabled: enabled && !!id,
    placeholderData: (prev) => prev,
    staleTime: 10_000,
  });

  return { calls: data?.items ?? [], total: data?.total ?? 0, loading, isFetching, refetch };
}

export function useWebhookCallDetail(webhookId: string | null, callId: string | null) {
  const http = useHttp();

  const { data, isLoading: loading } = useQuery({
    queryKey: queryKeys.webhooks.call(webhookId ?? "", callId ?? ""),
    queryFn: () => http.get<WebhookCallDetail>(`/v1/webhooks/${webhookId}/calls/${callId}`),
    enabled: !!webhookId && !!callId,
    staleTime: 30_000,
  });

  return { detail: data ?? null, loading };
}
