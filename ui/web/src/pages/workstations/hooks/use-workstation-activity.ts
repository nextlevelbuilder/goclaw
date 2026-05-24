import { useState, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";

export interface WorkstationActivity {
  id: string;
  tenantId: string;
  workstationId: string;
  agentId: string;
  action: "exec" | "deny";
  cmdHash: string;
  cmdPreview: string;
  exitCode: number | null;
  durationMs: number | null;
  denyReason: string;
  createdAt: string;
}

interface UseWorkstationActivityResult {
  rows: WorkstationActivity[];
  loading: boolean;
  error: string | null;
  hasMore: boolean;
  load: (workstationId: string) => Promise<void>;
  loadMore: () => Promise<void>;
}

export function useWorkstationActivity(): UseWorkstationActivityResult {
  const ws = useWs();
  const [rows, setRows] = useState<WorkstationActivity[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [hasMore, setHasMore] = useState(false);
  const [currentWsId, setCurrentWsId] = useState<string | null>(null);

  const load = useCallback(
    async (workstationId: string) => {
      setLoading(true);
      setError(null);
      setCurrentWsId(workstationId);
      setCursor(undefined);
      try {
        const res = await ws.call<{
          activity: WorkstationActivity[];
          nextCursor?: string;
        }>(Methods.WORKSTATIONS_LIST_ACTIVITY, {
          workstationId,
          limit: 50,
        });
        setRows(res.activity ?? []);
        setCursor(res.nextCursor);
        setHasMore(!!res.nextCursor);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load activity");
      } finally {
        setLoading(false);
      }
    },
    [ws],
  );

  const loadMore = useCallback(async () => {
    if (!currentWsId || !cursor || loading) return;
    setLoading(true);
    try {
      const res = await ws.call<{
        activity: WorkstationActivity[];
        nextCursor?: string;
      }>(Methods.WORKSTATIONS_LIST_ACTIVITY, {
        workstationId: currentWsId,
        limit: 50,
        cursor,
      });
      setRows((prev) => [...prev, ...(res.activity ?? [])]);
      setCursor(res.nextCursor);
      setHasMore(!!res.nextCursor);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load more activity");
    } finally {
      setLoading(false);
    }
  }, [ws, currentWsId, cursor, loading]);

  return { rows, loading, error, hasMore, load, loadMore };
}
