import { useState, useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import { userFriendlyError } from "@/lib/error-utils";
import { ApiError } from "@/api/errors";
import i18n from "@/i18n";
import type { AgentShareData } from "@/types/agent";

export function useAgentShares(agentId: string | undefined) {
  const http = useHttp();
  const [shares, setShares] = useState<AgentShareData[]>([]);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    if (!agentId) return;
    setLoading(true);
    try {
      const res = await http.get<{ shares: AgentShareData[] }>(`/v1/agents/${agentId}/shares`);
      setShares(res.shares ?? []);
    } catch (err) {
      // Owner-only endpoint; silently handle 403/404
      if (err instanceof ApiError && (err.code === "UNAUTHORIZED" || err.code === "NOT_FOUND")) {
        setShares([]);
      } else {
        setShares([]);
        toast.error(i18n.t("agents:toast.updateFailed"), userFriendlyError(err));
      }
    } finally {
      setLoading(false);
    }
  }, [agentId, http]);

  const share = useCallback(async (userId: string, role: string) => {
    if (!agentId) return;
    try {
      await http.post(`/v1/agents/${agentId}/shares`, { user_id: userId, role });
      toast.success(i18n.t("agents:toast.updated"));
      await load();
    } catch (err) {
      toast.error(i18n.t("agents:toast.updateFailed"), userFriendlyError(err));
    }
  }, [agentId, http, load]);

  const revoke = useCallback(async (userId: string) => {
    if (!agentId) return;
    try {
      await http.delete(`/v1/agents/${agentId}/shares/${userId}`);
      toast.success(i18n.t("agents:toast.updated"));
      await load();
    } catch (err) {
      toast.error(i18n.t("agents:toast.updateFailed"), userFriendlyError(err));
    }
  }, [agentId, http, load]);

  return { shares, loading, load, share, revoke };
}
