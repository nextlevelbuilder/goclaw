import { useEffect, useCallback, useRef } from "react";
import { useWs } from "@/hooks/use-ws";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Methods } from "@/api/protocol";
import {
  useLogsStore,
  type LogEntry,
  type LogLevel,
} from "@/stores/use-logs-store";

/**
 * Bridge hook: connects WsClient (React context) to the global Zustand log store.
 * Log state persists across page navigation — tailing continues in background.
 * Mount this hook on the logs page; the store retains entries when unmounted.
 */
export function useLogs() {
  const ws = useWs();
  const store = useLogsStore();
  const tailingRef = useRef(store.tailing);
  tailingRef.current = store.tailing;

  const startTail = useCallback(
    async (level?: LogLevel) => {
      if (!ws.isConnected) return;
      const lvl = level ?? store.level;
      store.setError(null);
      try {
        await ws.call(Methods.LOGS_TAIL, { action: "start", level: lvl });
        store.setTailing(true);
        store.setLevel(lvl);
      } catch {
        store.setError("logs.tail is not available on this backend.");
      }
    },
    [ws, store],
  );

  const stopTail = useCallback(async () => {
    if (!ws.isConnected) return;
    try {
      await ws.call(Methods.LOGS_TAIL, { action: "stop" });
    } catch {
      // ignore
    }
    store.setTailing(false);
  }, [ws, store]);

  // Listen for log events — always active while this hook is mounted.
  useWsEvent(
    "log",
    useCallback(
      (payload: unknown) => {
        const entry = payload as LogEntry;
        if (entry) useLogsStore.getState().appendLog(entry);
      },
      [],
    ),
  );

  // On WS reconnect: re-subscribe if tailing was active.
  const prevConnected = useRef(ws.isConnected);
  useEffect(() => {
    if (ws.isConnected && !prevConnected.current && tailingRef.current) {
      // Reconnected while tailing — re-subscribe.
      ws.call(Methods.LOGS_TAIL, {
        action: "start",
        level: useLogsStore.getState().level,
      }).catch((err) => console.error("[useLogs] reconnect tail failed:", err));
    }
    prevConnected.current = ws.isConnected;
  }, [ws, ws.isConnected]);

  return {
    logs: store.logs,
    tailing: store.tailing,
    level: store.level,
    error: store.error,
    startTail,
    stopTail,
    clearLogs: store.clearLogs,
  };
}

export type { LogEntry, LogLevel };
