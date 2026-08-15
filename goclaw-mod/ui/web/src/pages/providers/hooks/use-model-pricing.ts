import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import type { PricingCatalogEntry, PricingOverride, UsagePricingFields } from "@/types/usage-caps";

export interface PricingOverrideInput {
  provider_id: string;
  provider_type: string;
  model_id: string;
  pricing: UsagePricingFields;
  enabled?: boolean;
}

export function useModelPricing(providerId: string, modelSearch: string) {
  const http = useHttp();
  const queryClient = useQueryClient();
  const pricingKey = queryKeys.providers.pricing(providerId);

  const catalogQuery = useQuery({
    queryKey: queryKeys.providers.pricingCatalog(modelSearch),
    queryFn: async () => {
      const res = await http.get<{ models: PricingCatalogEntry[] }>("/v1/model-pricing", { model: modelSearch, limit: "8" });
      return res.models ?? [];
    },
  });

  const overridesQuery = useQuery({
    queryKey: pricingKey,
    queryFn: async () => {
      const res = await http.get<{ overrides: PricingOverride[] }>("/v1/model-pricing/overrides", { provider_id: providerId });
      return res.overrides ?? [];
    },
  });

  const invalidate = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: pricingKey }),
      queryClient.invalidateQueries({ queryKey: queryKeys.providers.pricingCatalog(modelSearch) }),
    ]);
  }, [modelSearch, pricingKey, queryClient]);

  const syncOpenRouter = useCallback(async () => {
    try {
      const res = await http.post<{ count: number }>("/v1/model-pricing/sync-openrouter");
      await invalidate();
      toast.success(i18next.t("providers:pricing.toast.synced", { count: res.count }));
    } catch (err) {
      toast.error(i18next.t("providers:pricing.toast.syncFailed"), err instanceof Error ? err.message : "");
      throw err;
    }
  }, [http, invalidate]);

  const saveOverride = useCallback(
    async (input: PricingOverrideInput) => {
      try {
        await http.put<PricingOverride>("/v1/model-pricing/overrides", input);
        await invalidate();
        toast.success(i18next.t("providers:pricing.toast.saved"));
      } catch (err) {
        toast.error(i18next.t("providers:pricing.toast.saveFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteOverride = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/model-pricing/overrides/${id}`);
        await invalidate();
        toast.success(i18next.t("providers:pricing.toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("providers:pricing.toast.deleteFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  return {
    catalog: catalogQuery.data ?? [],
    overrides: overridesQuery.data ?? [],
    loading: catalogQuery.isLoading || overridesQuery.isLoading,
    refreshing: catalogQuery.isFetching || overridesQuery.isFetching,
    syncOpenRouter,
    saveOverride,
    deleteOverride,
  };
}
