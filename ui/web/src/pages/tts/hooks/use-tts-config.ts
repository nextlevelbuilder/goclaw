import { useState, useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
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

export interface SynthesizeParams {
  text: string;
  provider?: string;
  voice_id?: string;
  model_id?: string;
}

export interface TestConnectionParams {
  provider: string;
  api_key?: string;
  api_base?: string;
  voice_id?: string;
  model_id?: string;
  group_id?: string;
}

export interface TestConnectionResult {
  success: boolean;
  provider?: string;
  latency_ms?: number;
  error?: string;
}

export function useTtsConfig() {
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const queryClient = useQueryClient();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { data: tts = DEFAULT_TTS, isPending: loading } = useQuery({
    queryKey: queryKeys.tts.all,
    queryFn: async () => {
      // Use per-tenant TTS config endpoint instead of global config.get
      const res = await fetch("/v1/tts/config", {
        headers: http.getAuthHeaders(),
      });
      if (!res.ok) {
        throw new Error(`Failed to load TTS config (${res.status})`);
      }
      const data = await res.json();
      return { ...DEFAULT_TTS, ...data } as TtsConfig;
    },
    staleTime: 60_000,
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
        // Use per-tenant TTS config endpoint instead of config.patch (master-scope)
        const res = await fetch("/v1/tts/config", {
          method: "POST",
          headers: { "Content-Type": "application/json", ...http.getAuthHeaders() },
          body: JSON.stringify(updates),
        });
        if (!res.ok) {
          const text = await res.text().catch(() => "");
          throw new Error(text || `Failed to save TTS config (${res.status})`);
        }
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
    [http, invalidate],
  );

  // POST→Blob not in HttpClient; use fetch + getAuthHeaders() for tenant/user header parity.
  // See: http-client.ts:107-109 — getAuthHeaders() returns Authorization + X-GoClaw-* headers.
  const synthesize = useCallback(
    async (params: SynthesizeParams): Promise<Blob> => {
      const res = await fetch("/v1/tts/synthesize", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...http.getAuthHeaders() },
        body: JSON.stringify(params),
      });
      if (!res.ok) {
        const text = await res.text().catch(() => "");
        throw new Error(text || `Synthesis failed (${res.status})`);
      }
      return res.blob();
    },
    [http],
  );

  // Test connection with unsaved credentials — uses ephemeral provider.
  const testConnection = useCallback(
    async (params: TestConnectionParams): Promise<TestConnectionResult> => {
      const res = await fetch("/v1/tts/test-connection", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...http.getAuthHeaders() },
        body: JSON.stringify(params),
      });
      const data = (await res.json()) as TestConnectionResult;
      if (!res.ok || !data.success) {
        throw new Error(data.error || `Test failed (${res.status})`);
      }
      return data;
    },
    [http],
  );

  return { tts, loading, saving, error, refresh: invalidate, save, synthesize, testConnection };
}
