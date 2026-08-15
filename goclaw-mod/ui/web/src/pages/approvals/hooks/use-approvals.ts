import { useState, useEffect, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Methods, Events } from "@/api/protocol";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";

export interface PendingApproval {
  id: string;
  command: string;
  agentId: string;
  createdAt: number;
}

export function useApprovals() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [pending, setPending] = useState<PendingApproval[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!connected) return;
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
  }, [ws, connected]);

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
        toast.success(i18n.t("approvals:toast.approved"), always ? i18n.t("approvals:toast.approvedAlways") : i18n.t("approvals:toast.approvedOnce"));
      } catch (err) {
        toast.error(i18n.t("approvals:toast.approveFailed"), err instanceof Error ? err.message : i18n.t("approvals:toast.unknownError"));
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
        toast.success(i18n.t("approvals:toast.denied"), i18n.t("approvals:toast.deniedDesc"));
      } catch (err) {
        toast.error(i18n.t("approvals:toast.denyFailed"), err instanceof Error ? err.message : i18n.t("approvals:toast.unknownError"));
        throw err;
      }
    },
    [ws],
  );

  return { pending, loading, error, refresh: load, approve, deny };
}
