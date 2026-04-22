import { useQuery } from "@tanstack/react-query";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";

/**
 * ConfigDefaults mirrors the backend response of `config.defaults` RPC.
 * Shape is the SSoT nested form matching TS ContextPruningConfig / SubagentsConfig.
 */
export interface ConfigDefaults {
  agents: {
    contextPruning: {
      keepLastAssistants: number;
      softTrimRatio: number;
      hardClearRatio: number;
      minPrunableToolChars: number;
      ttl: string;
      softTrim: {
        maxChars: number;
        headChars: number;
        tailChars: number;
      };
      hardClear: {
        enabled: boolean;
        placeholder: string;
      };
    };
    subagents: {
      maxConcurrent: number;
      maxSpawnDepth: number;
      maxChildrenPerAgent: number;
      archiveAfterMinutes: number;
      maxRetries: number;
    };
  };
}

// Fallback values sync'd once from Go consts (internal/agent/pruning.go,
// internal/tools/subagent_config.go). Used as initialData so placeholders render
// before the WS response lands and also when the gateway is unreachable.
export const CONFIG_DEFAULTS_FALLBACK: ConfigDefaults = {
  agents: {
    contextPruning: {
      keepLastAssistants: 3,
      softTrimRatio: 0.25,
      hardClearRatio: 0.5,
      minPrunableToolChars: 50000,
      ttl: "5m",
      softTrim: { maxChars: 6000, headChars: 3000, tailChars: 3000 },
      hardClear: { enabled: true, placeholder: "[Old tool result content cleared]" },
    },
    subagents: {
      maxConcurrent: 8,
      maxSpawnDepth: 1,
      maxChildrenPerAgent: 5,
      archiveAfterMinutes: 60,
      maxRetries: 2,
    },
  },
};

/**
 * useConfigDefaults returns the system-level defaults for agent config fields.
 * Placeholder values in the UI should derive from this hook so they always
 * match what the backend will apply when the user leaves a field empty.
 */
export function useConfigDefaults() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);

  const { data } = useQuery<ConfigDefaults>({
    queryKey: queryKeys.config.defaults,
    queryFn: () => ws.call<ConfigDefaults>(Methods.CONFIG_DEFAULTS),
    staleTime: 60_000,
    enabled: connected,
    initialData: CONFIG_DEFAULTS_FALLBACK,
  });

  return data;
}
