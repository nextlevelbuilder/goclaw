import { useState, useEffect, useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import type { AgentShareData } from "@/types/agent";

export function useAgentShares(agentId: string) {
  const http = useHttp();
  const [shares, setShares] = useState<AgentShareData[]>([]);
  const [loading, setLoading] = useState(false);

  const loadShares = useCallback(async () => {
    setLoading(true);
    try {
      const res = await http.get<{ shares: AgentShareData[] }>(
        `/v1/agents/${agentId}/shares`,
      );
      setShares(res.shares ?? []);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [http, agentId]);

  useEffect(() => {
    loadShares();
  }, [loadShares]);

  const addShare = useCallback(
    async (userId: string, role: string) => {
      await http.post(`/v1/agents/${agentId}/shares`, {
        user_id: userId,
        role,
      });
      loadShares();
    },
    [http, agentId, loadShares],
  );

  const revokeShare = useCallback(
    async (userId: string) => {
      await http.delete(`/v1/agents/${agentId}/shares/${userId}`);
      loadShares();
    },
    [http, agentId, loadShares],
  );

  return { shares, loading, refresh: loadShares, addShare, revokeShare };
}
