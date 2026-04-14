import { useState, useRef } from "react";
import { useWsEvent } from "@/hooks/use-ws-event";

export interface EnrichmentEvent {
  phase: string;   // enriching, complete, error
  done: number;
  total: number;
  running: boolean;
  error_count?: number;
  last_error?: string;
}

/**
 * Listens to vault.enrich.progress WS events and returns current enrichment state.
 * Progress auto-clears 3s after completion. Stale timers are cancelled when
 * new events arrive to prevent progress bar from disappearing mid-enrichment.
 */
export function useEnrichmentProgress() {
  const [event, setEvent] = useState<EnrichmentEvent | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(null);

  useWsEvent("vault.enrich.progress", (payload) => {
    const data = payload as EnrichmentEvent;

    // Cancel any pending clear timer from a previous "complete" event.
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }

    setEvent(data);

    if (!data.running) {
      timerRef.current = setTimeout(() => {
        setEvent(null);
        timerRef.current = null;
      }, 3000);
    }
  });

  return event;
}
