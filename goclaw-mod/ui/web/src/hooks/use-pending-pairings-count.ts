import { useState, useEffect, useCallback } from "react";
import { useWs } from "./use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { useWsEvent } from "./use-ws-event";
import { Methods, Events } from "@/api/protocol";
import { toast } from "@/stores/use-toast-store";

interface Options {
  /** Show toast on new pairing request. Enable in only ONE call site to avoid duplicates. */
  showToast?: boolean;
}

export function usePendingPairingsCount({ showToast }: Options = {}) {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [pendingCount, setPendingCount] = useState(0);

  const fetchCount = useCallback(async () => {
    if (!connected) return;
    try {
      const res = await ws.call<{ pending: { code: string }[] }>(
        Methods.PAIRING_LIST,
      );
      setPendingCount(res.pending?.length ?? 0);
    } catch {
      // ignore
    }
  }, [ws, connected]);

  useEffect(() => {
    fetchCount();
  }, [fetchCount]);

  useWsEvent(
    Events.DEVICE_PAIR_REQUESTED,
    useCallback(
      (payload: unknown) => {
        fetchCount();
        if (showToast) {
          const p = payload as Record<string, string> | undefined;
          toast.info(
            "New pairing request",
            `${p?.channel ?? "device"} — ${p?.sender_id ?? "unknown"}`,
          );
        }
      },
      [fetchCount, showToast],
    ),
  );

  useWsEvent(
    Events.DEVICE_PAIR_RESOLVED,
    useCallback(() => {
      fetchCount();
    }, [fetchCount]),
  );

  return { pendingCount };
}
