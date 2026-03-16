import { useEffect } from "react";
import { useAuthStore } from "@/stores/use-auth-store";
import { useFeaturesStore } from "@/stores/use-features-store";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";

/**
 * Syncs feature flags from server config into the features store.
 * Call this once in the app layout to keep features in sync.
 */
export function useFeaturesSync() {
  const connected = useAuthStore((s) => s.connected);
  const setFeatures = useFeaturesStore((s) => s.setFeatures);
  const ws = useWs();

  useEffect(() => {
    if (!connected) return;

    ws.call<{ config?: { features?: Record<string, boolean> } }>(Methods.CONFIG_GET)
      .then((res) => {
        if (res?.config?.features) {
          setFeatures(res.config.features);
        }
      })
      .catch(() => {
        // Ignore — features default to all enabled
      });
  }, [connected, ws, setFeatures]);
}
