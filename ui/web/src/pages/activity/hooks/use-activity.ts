import { useState, useEffect, useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";

export interface ActivityLog {
  id: string;
  actor_type: string;
  actor_id: string;
  action: string;
  entity_type: string;
  entity_id: string;
  details: unknown;
  ip_address: string;
  created_at: string;
}

interface ActivityListResponse {
  logs: ActivityLog[];
  total: number;
  limit: number;
  offset: number;
}

interface ActivityFilters {
  action?: string;
  entity_type?: string;
  actor_id?: string;
  limit?: number;
  offset?: number;
}

export function useActivity() {
  const http = useHttp();
  const [logs, setLogs] = useState<ActivityLog[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);

  const load = useCallback(
    async (filters?: ActivityFilters) => {
      setLoading(true);
      try {
        const params: Record<string, string> = {};
        if (filters?.action) params.action = filters.action;
        if (filters?.entity_type) params.entity_type = filters.entity_type;
        if (filters?.actor_id) params.actor_id = filters.actor_id;
        if (filters?.limit) params.limit = String(filters.limit);
        if (filters?.offset) params.offset = String(filters.offset);

        const res = await http.get<ActivityListResponse>("/v1/activity", params);
        setLogs(res.logs ?? []);
        setTotal(res.total ?? 0);
      } catch {
        // ignore
      } finally {
        setLoading(false);
      }
    },
    [http],
  );

  useEffect(() => {
    load();
  }, [load]);

  return { logs, total, loading, load };
}
