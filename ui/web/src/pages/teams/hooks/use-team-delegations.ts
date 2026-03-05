import { useState, useEffect, useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import type { DelegationHistoryRecord } from "@/types/delegation";

export function useTeamDelegations(teamId: string) {
  const http = useHttp();
  const [records, setRecords] = useState<DelegationHistoryRecord[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await http.get<{ records: DelegationHistoryRecord[] }>("/v1/delegations", {
        team_id: teamId,
        limit: "50",
      });
      setRecords(res.records ?? []);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [http, teamId]);

  useEffect(() => {
    load();
  }, [load]);

  return { records, loading, refresh: load };
}
