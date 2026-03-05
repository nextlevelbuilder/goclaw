import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useWsEvent } from "./use-ws-event";
import { Events } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import type { AgentEventPayload } from "@/types/chat";

/**
 * Listens to WebSocket events and invalidates relevant TanStack Query caches.
 * Mount once at app level (e.g., in WsProvider or AppProviders).
 */
export function useWsQueryInvalidation() {
  const queryClient = useQueryClient();

  // When an agent run completes/fails → refresh sessions + traces + usage
  const handleAgentEvent = useCallback(
    (payload: unknown) => {
      const event = payload as AgentEventPayload;
      if (!event) return;
      if (event.type === "run.completed" || event.type === "run.failed") {
        queryClient.invalidateQueries({ queryKey: queryKeys.sessions.all });
        queryClient.invalidateQueries({ queryKey: queryKeys.traces.all });
        queryClient.invalidateQueries({ queryKey: queryKeys.usage.all });
      }
    },
    [queryClient],
  );

  // Cron events → refresh cron jobs list
  const handleCronEvent = useCallback(
    () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.cron.all });
    },
    [queryClient],
  );

  // Health events → refresh agents list (agent status may have changed)
  const handleHealthEvent = useCallback(
    () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.agents.all });
    },
    [queryClient],
  );

  useWsEvent(Events.AGENT, handleAgentEvent);
  useWsEvent(Events.CRON, handleCronEvent);
  useWsEvent(Events.HEALTH, handleHealthEvent);
}
