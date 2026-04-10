import { useState } from "react";
import { useWsEvent } from "@/hooks/use-ws-event";

export interface EnrichmentEvent {
  phase: string;   // enriching, complete
  done: number;
  total: number;
  running: boolean;
}

/**
 * Listens to vault.enrich.progress WS events and returns current enrichment state.
 * Progress auto-clears 3s after completion.
 */
export function useEnrichmentProgress() {
  const [event, setEvent] = useState<EnrichmentEvent | null>(null);

  useWsEvent("vault.enrich.progress", (payload) => {
    const data = payload as EnrichmentEvent;
    setEvent(data);
    if (!data.running) {
      setTimeout(() => setEvent(null), 3000);
    }
  });

  return event;
}
