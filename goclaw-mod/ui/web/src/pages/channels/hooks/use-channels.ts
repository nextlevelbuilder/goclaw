import { useState, useEffect, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import type { ChannelRuntimeStatus } from "@/types/channel";

export type ChannelStatus = ChannelRuntimeStatus;

export function useChannels() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [channels, setChannels] = useState<Record<string, ChannelStatus>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!connected) return;
    setLoading(true);
    setError(null);
    try {
      const res = await ws.call<{ channels: Record<string, ChannelStatus> }>(
        Methods.CHANNELS_STATUS,
      );
      setChannels(res.channels ?? {});
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load channels");
    } finally {
      setLoading(false);
    }
  }, [ws, connected]);

  useEffect(() => {
    load();
  }, [load]);

  return { channels, loading, error, refresh: load };
}
