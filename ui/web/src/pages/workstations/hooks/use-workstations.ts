import { useState, useEffect, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";

export interface Workstation {
  id: string;
  workstation_key: string;
  name: string;
  backend_type: "ssh" | "docker";
  active: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateWorkstationParams {
  workstation_key: string;
  name: string;
  backend_type: "ssh" | "docker";
  metadata?: Record<string, unknown>;
}

export interface UpdateWorkstationParams {
  name?: string;
  active?: boolean;
  metadata?: Record<string, unknown>;
}

export function useWorkstations() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [workstations, setWorkstations] = useState<Workstation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!connected) return;
    setLoading(true);
    setError(null);
    try {
      const res = await ws.call<{ workstations: Workstation[] }>(Methods.WORKSTATIONS_LIST);
      setWorkstations(res.workstations ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load workstations");
    } finally {
      setLoading(false);
    }
  }, [ws, connected]);

  useEffect(() => {
    load();
  }, [load]);

  const createWorkstation = useCallback(
    async (params: CreateWorkstationParams): Promise<Workstation> => {
      const res = await ws.call<{ workstation: Workstation }>(Methods.WORKSTATIONS_CREATE, params as unknown as Record<string, unknown>);
      await load();
      return res.workstation;
    },
    [ws, load],
  );

  const updateWorkstation = useCallback(
    async (id: string, params: UpdateWorkstationParams): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_UPDATE, { id, ...params });
      await load();
    },
    [ws, load],
  );

  const deleteWorkstation = useCallback(
    async (id: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_DELETE, { id });
      await load();
    },
    [ws, load],
  );

  return {
    workstations,
    loading,
    error,
    refresh: load,
    createWorkstation,
    updateWorkstation,
    deleteWorkstation,
  };
}
