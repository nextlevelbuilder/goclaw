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
  requires: string[];
  metadata: Record<string, unknown>;
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
        await http.put(`/v1/tools/builtin/${name}/tenant-config`, { enabled });
        await invalidate();
        toast.success(i18next.t("tools:builtin.settingsDialog.toast.saved"));
      } catch (err) {
        toast.error(i18next.t("tools:builtin.settingsDialog.toast.failed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
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

  return { tools, loading, refresh: invalidate, updateTool, setTenantConfig, deleteTenantConfig };
}
