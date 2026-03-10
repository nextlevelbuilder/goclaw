import { useState, useCallback, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";

interface ConfigData {
  config: Record<string, unknown>;
  hash: string;
  path: string;
}

export function useConfig() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const queryClient = useQueryClient();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const hashRef = useRef("");

  const { data, isPending: loading } = useQuery({
    queryKey: queryKeys.config.all,
    queryFn: async (): Promise<ConfigData> => {
      const res = await ws.call<ConfigData>(Methods.CONFIG_GET);
      hashRef.current = res.hash;
      return res;
    },
    staleTime: 5 * 60_000,
    enabled: connected,
  });

  const config = data?.config ?? null;
  const hash = data?.hash ?? "";
  const configPath = data?.path ?? "";

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.config.all }),
    [queryClient],
  );

  const applyRaw = useCallback(
    async (raw: string) => {
      setSaving(true);
      setError(null);
      try {
        const res = await ws.call<{ hash: string }>(Methods.CONFIG_APPLY, {
          raw,
          baseHash: hashRef.current,
        });
        hashRef.current = res.hash;
        await invalidate();
        toast.success("Config saved");
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Failed to apply config";
        setError(msg);
        toast.error("Failed to save config", msg);
        throw err;
      } finally {
        setSaving(false);
      }
    },
    [ws, invalidate],
  );

  const patch = useCallback(
    async (updates: Record<string, unknown>) => {
      setSaving(true);
      setError(null);
      try {
        const res = await ws.call<{ hash: string }>(Methods.CONFIG_PATCH, {
          raw: JSON.stringify(updates),
          baseHash: hashRef.current,
        });
        hashRef.current = res.hash;
        await invalidate();
        toast.success("Config saved");
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Failed to patch config";
        setError(msg);
        toast.error("Failed to save config", msg);
        throw err;
      } finally {
        setSaving(false);
      }
    },
    [ws, invalidate],
  );

  return { config, hash, configPath, loading, saving, error, refresh: invalidate, applyRaw, patch };
}
