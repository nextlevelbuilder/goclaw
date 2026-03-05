import { useState, useEffect, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Methods, Events } from "@/api/protocol";
import { toast } from "@/stores/use-toast-store";

export interface PendingApproval {
  id: string;
  command: string;
  agentId: string;
  createdAt: number;
}

export function useApprovals() {
  const ws = useWs();
  const [pending, setPending] = useState<PendingApproval[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!ws.isConnected) return;
    setLoading(true);
    setError(null);
    try {
      const res = await ws.call<{ pending: PendingApproval[] }>(Methods.APPROVALS_LIST);
      setPending(res.pending ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load approvals");
    } finally {
      setLoading(false);
    }
  }, [ws]);

  useEffect(() => {
    load();
  }, [load]);

  // Listen for new approval requests
  useWsEvent(Events.EXEC_APPROVAL_REQUESTED, load);

  // Listen for resolved approvals
  useWsEvent(Events.EXEC_APPROVAL_RESOLVED, load);

  const approve = useCallback(
    async (id: string, always = false) => {
      try {
        await ws.call(Methods.APPROVALS_APPROVE, { id, always });
        setPending((prev) => prev.filter((a) => a.id !== id));
        toast.success("Approved", always ? "Command always allowed" : "Command approved");
      } catch (err) {
        toast.error("Failed to approve", err instanceof Error ? err.message : "Unknown error");
        throw err;
      }
    },
    [ws],
  );

  const deny = useCallback(
    async (id: string) => {
      try {
        await ws.call(Methods.APPROVALS_DENY, { id });
        setPending((prev) => prev.filter((a) => a.id !== id));
        toast.success("Denied", "Command denied");
      } catch (err) {
        toast.error("Failed to deny", err instanceof Error ? err.message : "Unknown error");
        throw err;
      }
    },
    [ws],
  );

  return { pending, loading, error, refresh: load, approve, deny };
}
