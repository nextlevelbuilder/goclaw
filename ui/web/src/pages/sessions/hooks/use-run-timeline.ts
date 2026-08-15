import { useQuery } from "@tanstack/react-query";
import { Methods } from "@/api/protocol";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { queryKeys } from "@/lib/query-keys";
import type { RunTimelineResponse } from "@/types/run-timeline";

interface UseRunTimelineOptions {
  runId?: string;
  sessionKey?: string;
  limit?: number;
}

export function useRunTimeline({ runId, sessionKey, limit = 100 }: UseRunTimelineOptions) {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);

  return useQuery({
    queryKey: queryKeys.sessions.timeline({ runId, sessionKey, limit }),
    queryFn: async () => {
      if (!ws.isConnected) return { items: [], limit, offset: 0 } satisfies RunTimelineResponse;
      return ws.call<RunTimelineResponse>(Methods.RUN_TIMELINE_GET, {
        runId,
        sessionKey,
        limit,
      });
    },
    staleTime: 10_000,
    enabled: connected && Boolean(runId || sessionKey),
  });
}
