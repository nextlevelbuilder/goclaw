import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";

export interface BuiltinToolData {
  name: string;
  display_name: string;
  description: string;
  category: string;
  enabled: boolean;
  tenant_enabled: boolean | null;
  settings: Record<string, unknown>;
  // Tenant override for settings JSON. Null when no tenant override
  // exists (row in builtin_tool_tenant_configs has settings column NULL
  // or row absent). Present only when request is tenant-scoped.
  tenant_settings: Record<string, unknown> | null;
  requires: string[];
  metadata: Record<string, unknown>;
  // Boolean status map for tool API keys. Raw values are never returned.
  // Key format: "tools.web.<provider>.api_key". Present only for tools with secrets.
  secrets_set?: Record<string, boolean>;
  created_at: string;
  updated_at: string;
}

export function useBuiltinTools() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: tools = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.builtinTools.all,
    queryFn: async () => {
      const res = await http.get<{ tools: BuiltinToolData[] }>("/v1/tools/builtin");
      return res.tools ?? [];
    },
    staleTime: 5 * 60_000,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.builtinTools.all }),
    [queryClient],
  );

  const updateTool = useCallback(
    async (name: string, data: { enabled?: boolean; settings?: Record<string, unknown> }) => {
      try {
        if (data.enabled !== undefined) {
          queryClient.setQueryData<BuiltinToolData[]>(queryKeys.builtinTools.all, (old) =>
            old?.map((t) => (t.name === name ? { ...t, enabled: data.enabled! } : t)),
          );
        }
        await http.put(`/v1/tools/builtin/${name}`, data);
        await invalidate();
        toast.success(i18next.t("tools:builtin.settingsDialog.toast.saved"));
      } catch (err) {
        toast.error(i18next.t("tools:builtin.settingsDialog.toast.failed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const setTenantConfig = useCallback(
    async (name: string, enabled: boolean) => {
      try {
        queryClient.setQueryData<BuiltinToolData[]>(queryKeys.builtinTools.all, (old) =>
          old?.map((t) => (t.name === name ? { ...t, tenant_enabled: enabled } : t)),
        );
        await http.put(`/v1/tools/builtin/${name}/tenant-config`, { enabled });
        await invalidate();
        toast.success(i18next.t("tools:builtin.settingsDialog.toast.saved"));
      } catch (err) {
        toast.error(i18next.t("tools:builtin.settingsDialog.toast.failed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, queryClient, invalidate],
  );

  // setTenantSettings writes the JSONB `settings` column of
  // builtin_tool_tenant_configs for the current tenant. Passing `null`
  // clears the override (backend maps literal `null` → SQL NULL) while
  // preserving the `enabled` column on the same row.
  const setTenantSettings = useCallback(
    async (name: string, settings: Record<string, unknown> | null) => {
      try {
        queryClient.setQueryData<BuiltinToolData[]>(queryKeys.builtinTools.all, (old) =>
          old?.map((t) => (t.name === name ? { ...t, tenant_settings: settings } : t)),
        );
        await http.put(`/v1/tools/builtin/${name}/tenant-config`, { settings });
        await invalidate();
        toast.success(i18next.t("tools:builtin.settingsDialog.toast.saved"));
      } catch (err) {
        toast.error(i18next.t("tools:builtin.settingsDialog.toast.failed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, queryClient, invalidate],
  );

  const clearTenantSettings = useCallback(
    (name: string) => setTenantSettings(name, null),
    [setTenantSettings],
  );

  const deleteTenantConfig = useCallback(
    async (name: string) => {
      try {
        await http.delete(`/v1/tools/builtin/${name}/tenant-config`);
        await invalidate();
        toast.success(i18next.t("tools:builtin.settingsDialog.toast.saved"));
      } catch (err) {
        toast.error(i18next.t("tools:builtin.settingsDialog.toast.failed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  return {
    tools,
    loading,
    refresh: invalidate,
    updateTool,
    setTenantConfig,
    deleteTenantConfig,
    setTenantSettings,
    clearTenantSettings,
  };
}
