import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import i18next from "i18next";
import { toast } from "@/stores/use-toast-store";

const DEFAULTS: Record<string, string> = {
  "mcp.health_fail_threshold": "3",
  "mcp.health_check_interval": "30",
  "mcp.max_reconnect_attempts": "10",
  "mcp.reconnect_cooldown": "300",
  "mcp.idle_timeout": "0",
};

export interface MCPGlobalHealthSettings {
  healthFailThreshold: string;
  healthCheckInterval: string;
  maxReconnectAttempts: string;
  reconnectCooldown: string;
  idleTimeout: string;
}

function getOr(configs: Record<string, string | undefined>, key: string): string {
  return configs[key] ?? DEFAULTS[key] ?? "";
}

export function useMCPGlobalSettings() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: settings, isLoading } = useQuery({
    queryKey: queryKeys.mcp.globalSettings,
    queryFn: async (): Promise<MCPGlobalHealthSettings> => {
      const configs = await http.get<Record<string, string>>("/v1/system-configs");
      return {
        healthFailThreshold: getOr(configs, "mcp.health_fail_threshold"),
        healthCheckInterval: getOr(configs, "mcp.health_check_interval"),
        maxReconnectAttempts: getOr(configs, "mcp.max_reconnect_attempts"),
        reconnectCooldown: getOr(configs, "mcp.reconnect_cooldown"),
        idleTimeout: getOr(configs, "mcp.idle_timeout"),
      };
    },
    staleTime: 60_000,
  });

  const save = useCallback(
    async (values: MCPGlobalHealthSettings, init: MCPGlobalHealthSettings) => {
      const updates: Record<string, string> = {};
      if (values.healthFailThreshold !== init.healthFailThreshold)
        updates["mcp.health_fail_threshold"] = values.healthFailThreshold;
      if (values.healthCheckInterval !== init.healthCheckInterval)
        updates["mcp.health_check_interval"] = values.healthCheckInterval;
      if (values.maxReconnectAttempts !== init.maxReconnectAttempts)
        updates["mcp.max_reconnect_attempts"] = values.maxReconnectAttempts;
      if (values.reconnectCooldown !== init.reconnectCooldown)
        updates["mcp.reconnect_cooldown"] = values.reconnectCooldown;
      if (values.idleTimeout !== init.idleTimeout)
        updates["mcp.idle_timeout"] = values.idleTimeout;

      if (Object.keys(updates).length === 0) return;

      for (const [key, value] of Object.entries(updates)) {
        await http.put(`/v1/system-configs/${key}`, { value });
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.mcp.globalSettings });
      toast.success(i18next.t("mcp:toast.updated"));
    },
    [http, queryClient],
  );

  return { settings, isLoading, save };
}
