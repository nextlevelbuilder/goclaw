import { useEffect, useRef } from "react";
import { useWs } from "./use-ws";

/**
 * Subscribe to a WebSocket event. Automatically unsubscribes on unmount.
 * Uses a ref for the handler to avoid re-subscribing on every render
 * when callers pass inline functions.
 */
export function useWsEvent(
  event: string,
  handler: (payload: unknown) => void,
): void {
  const ws = useWs();
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    const unsubscribe = ws.on(event, (payload: unknown) => {
      handlerRef.current(payload);
    });
    return unsubscribe;
  }, [ws, event]);
}
