import { useState, useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";

export interface TtsProviderConfig {
  api_key?: string;
  api_base?: string;
  base_url?: string;
  model?: string;
  voice?: string;
  voice_id?: string;
  model_id?: string;
  enabled?: boolean;
  rate?: string;
  group_id?: string;
}

export interface TtsConfig {
  provider: string;
  auto: string;
  mode: string;
  max_length: number;
  timeout_ms: number;
  openai: TtsProviderConfig;
  elevenlabs: TtsProviderConfig;
  edge: TtsProviderConfig;
  minimax: TtsProviderConfig;
}

const DEFAULT_TTS: TtsConfig = {
  provider: "",
  auto: "off",
  mode: "final",
  max_length: 1500,
  timeout_ms: 30000,
  openai: {},
  elevenlabs: {},
  edge: {},
  minimax: {},
};

export function useTtsConfig() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const queryClient = useQueryClient();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { data: tts = DEFAULT_TTS, isPending: loading } = useQuery({
    queryKey: queryKeys.tts.all,
    queryFn: async () => {
      const res = await ws.call<{ config: Record<string, unknown> }>(Methods.CONFIG_GET);
      const ttsConfig = (res.config?.tts as TtsConfig) ?? DEFAULT_TTS;
      return { ...DEFAULT_TTS, ...ttsConfig };
    },
    enabled: connected,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.tts.all }),
    [queryClient],
  );

  const save = useCallback(
    async (updates: Partial<TtsConfig>) => {
      setSaving(true);
      setError(null);
      try {
        await ws.call(Methods.CONFIG_PATCH, { raw: JSON.stringify({ tts: updates }) });
        await invalidate();
        toast.success(i18next.t("config:toast.saved"));
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Failed to save TTS config";
        setError(msg);
        toast.error(i18next.t("config:toast.saveFailed"), userFriendlyError(err));
        throw err;
      } finally {
        setSaving(false);
      }
    },
    [ws, invalidate],
  );

  return { tts, loading, saving, error, refresh: invalidate, save };
}
