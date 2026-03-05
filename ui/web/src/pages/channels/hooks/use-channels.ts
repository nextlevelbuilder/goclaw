import { useState, useEffect, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";

export interface ChannelStatus {
  enabled: boolean;
  running: boolean;
}

export function useChannels() {
  const ws = useWs();
  const [channels, setChannels] = useState<Record<string, ChannelStatus>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!ws.isConnected) return;
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
  }, [ws]);

  useEffect(() => {
    load();
  }, [load]);

  return { channels, loading, error, refresh: load };
}
