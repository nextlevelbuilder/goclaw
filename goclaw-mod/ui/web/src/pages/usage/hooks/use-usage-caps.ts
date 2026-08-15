import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { UsageCapEvent, UsageCapPolicy, UsageCapUtilization } from "@/types/usage-caps";

export interface UsageCapPolicyInput {
  agent_id?: string;
  provider_id?: string;
  provider_type?: string;
  model_id?: string;
  window: UsageCapPolicy["window"];
  max_tokens?: number | null;
  max_cost_usd?: number | null;
  enabled?: boolean;
}

export function useUsageCaps() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const policiesQuery = useQuery({
    queryKey: queryKeys.usage.caps.policies,
    queryFn: async () => {
      const res = await http.get<{ policies: UsageCapPolicy[] }>("/v1/usage-caps/policies");
      return res.policies ?? [];
    },
  });

  const utilizationQuery = useQuery({
    queryKey: queryKeys.usage.caps.utilization,
    queryFn: async () => {
      const res = await http.get<{ rows: UsageCapUtilization[] }>("/v1/usage-caps/utilization");
      return res.rows ?? [];
    },
  });

  const eventsQuery = useQuery({
    queryKey: queryKeys.usage.caps.events,
    queryFn: async () => {
      const res = await http.get<{ events: UsageCapEvent[] }>("/v1/usage-caps/events", { limit: "10" });
      return res.events ?? [];
    },
  });

  const refresh = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.usage.caps.policies }),
      queryClient.invalidateQueries({ queryKey: queryKeys.usage.caps.utilization }),
      queryClient.invalidateQueries({ queryKey: queryKeys.usage.caps.events }),
    ]);
  }, [queryClient]);

  const createPolicy = useCallback(
    async (input: UsageCapPolicyInput) => {
      try {
        await http.post<UsageCapPolicy>("/v1/usage-caps/policies", input);
        await refresh();
        toast.success(i18next.t("usage:caps.toast.created"));
      } catch (err) {
        toast.error(i18next.t("usage:caps.toast.createFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, refresh],
  );

  const deletePolicy = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/usage-caps/policies/${id}`);
        await refresh();
        toast.success(i18next.t("usage:caps.toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("usage:caps.toast.deleteFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, refresh],
  );

  const updatePolicy = useCallback(
    async (id: string, input: UsageCapPolicyInput) => {
      try {
        await http.patch<UsageCapPolicy>(`/v1/usage-caps/policies/${id}`, input);
        await refresh();
        toast.success(i18next.t("usage:caps.toast.updated"));
      } catch (err) {
        toast.error(i18next.t("usage:caps.toast.updateFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, refresh],
  );

  return {
    policies: policiesQuery.data ?? [],
    utilization: utilizationQuery.data ?? [],
    events: eventsQuery.data ?? [],
    loading: policiesQuery.isLoading || utilizationQuery.isLoading || eventsQuery.isLoading,
    refreshing: policiesQuery.isFetching || utilizationQuery.isFetching || eventsQuery.isFetching,
    refresh,
    createPolicy,
    updatePolicy,
    deletePolicy,
  };
}
