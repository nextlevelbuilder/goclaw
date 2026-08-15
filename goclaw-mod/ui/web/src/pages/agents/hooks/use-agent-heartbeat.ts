import { useState, useEffect, useCallback, useRef } from "react";
import { toast } from "@/stores/use-toast-store";
import { userFriendlyError } from "@/lib/error-utils";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";

/** Matches Go AgentHeartbeat JSON tags (camelCase). */
export interface HeartbeatConfig {
  id: string;
  agentId: string;
  enabled: boolean;
  intervalSec: number;
  ackMaxChars: number;
  maxRetries: number;
  providerId?: string;
  model?: string;
  isolatedSession: boolean;
  lightContext: boolean;
  activeHoursStart?: string;
  activeHoursEnd?: string;
  timezone?: string;
  channel?: string;
  chatId?: string;
  nextRunAt?: string;
  lastRunAt?: string;
  lastStatus?: string;
  lastError?: string;
  runCount: number;
  suppressCount: number;
}

export interface DeliveryTarget {
  channel: string;
  chatId: string;
  title?: string;
  kind: string; // "dm" | "group"
}

export interface HeartbeatLog {
  id: string;
  status: string;
  summary?: string;
  error?: string;
  durationMs?: number;
  inputTokens?: number;
  outputTokens?: number;
  skipReason?: string;
  ranAt: string;
}

/** Return type of useAgentHeartbeat, for passing as props. */
export type UseAgentHeartbeatReturn = ReturnType<typeof useAgentHeartbeat>;

export function useAgentHeartbeat(agentId: string) {
  const ws = useWs();
  const [config, setConfig] = useState<HeartbeatConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!ws.isConnected || !agentId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await ws.call<{ heartbeat: HeartbeatConfig | null }>(
        Methods.HEARTBEAT_GET,
        { agentId },
      );
      setConfig(res.heartbeat);
    } catch (err) {
      const msg = userFriendlyError(err);
      setError(msg);
      toast.error(msg);
    } finally {
      setLoading(false);
    }
  }, [ws, agentId]);

  useEffect(() => {
    if (ws.isConnected && agentId) {
      refresh();
    }
  }, [ws.isConnected, agentId, refresh]);

  // Auto-refresh when countdown reaches 0: poll every 5s until backend returns a new nextRunAt.
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  useEffect(() => {
    // Only poll when enabled and nextRunAt is in the past (or just passed).
    const nextRunAt = config?.nextRunAt;
    const enabled = config?.enabled;
    if (!enabled || !nextRunAt || !ws.isConnected) {
      if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null; }
      return;
    }

    const nextMs = new Date(nextRunAt).getTime();
    const nowMs = Date.now();
    if (nextMs > nowMs) {
      // Still in the future — schedule poll to start when it expires.
      if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null; }
      const delay = nextMs - nowMs + 1000; // 1s after expiry
      const timeout = setTimeout(() => {
        // Start polling.
        refresh();
        pollRef.current = setInterval(() => refresh(), 5000);
      }, delay);
      return () => { clearTimeout(timeout); if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null; } };
    }

    // Already expired — start polling immediately.
    if (!pollRef.current) {
      refresh();
      pollRef.current = setInterval(() => refresh(), 5000);
    }

    return () => { if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null; } };
  }, [config?.enabled, config?.nextRunAt, ws.isConnected, refresh]);

  const toggle = useCallback(
    async (enabled: boolean) => {
      if (!agentId) return;
      setSaving(true);
      try {
        await ws.call(Methods.HEARTBEAT_TOGGLE, { agentId, enabled });
        // Wait for backend to compute nextRunAt, then refresh.
        setTimeout(() => refresh(), 2000);
      } catch (err) {
        const msg = userFriendlyError(err);
        toast.error(msg);
      } finally {
        setSaving(false);
      }
    },
    [ws, agentId, refresh],
  );

  const update = useCallback(
    async (params: Partial<HeartbeatConfig>) => {
      if (!agentId) return;
      setSaving(true);
      setError(null);
      try {
        const res = await ws.call<{ heartbeat: HeartbeatConfig }>(
          Methods.HEARTBEAT_SET,
          { agentId, ...params },
        );
        setConfig(res.heartbeat);
        // Delayed refresh to pick up any backend-computed state (e.g. nextRunAt).
        setTimeout(() => refresh(), 2000);
      } catch (err) {
        const msg = userFriendlyError(err);
        setError(msg);
        toast.error(msg);
        throw err;
      } finally {
        setSaving(false);
      }
    },
    [ws, agentId, refresh],
  );

  const test = useCallback(async () => {
    if (!agentId) return;
    setSaving(true);
    try {
      await ws.call(Methods.HEARTBEAT_TEST, { agentId });
      toast.success("Test run triggered");
    } catch (err) {
      const msg = userFriendlyError(err);
      toast.error(msg);
    } finally {
      setSaving(false);
    }
  }, [ws, agentId]);

  const fetchLogs = useCallback(
    async (limit = 20, offset = 0): Promise<{ logs: HeartbeatLog[]; total: number }> => {
      if (!agentId) return { logs: [], total: 0 };
      const res = await ws.call<{ logs: HeartbeatLog[]; total: number }>(
        Methods.HEARTBEAT_LOGS,
        { agentId, limit, offset },
      );
      return { logs: res.logs ?? [], total: res.total ?? 0 };
    },
    [ws, agentId],
  );

  const getChecklist = useCallback(async (): Promise<string> => {
    if (!agentId) return "";
    const res = await ws.call<{ content: string }>(
      Methods.HEARTBEAT_CHECKLIST_GET,
      { agentId },
    );
    return res.content ?? "";
  }, [ws, agentId]);

  const setChecklist = useCallback(
    async (content: string) => {
      if (!agentId) return;
      await ws.call(Methods.HEARTBEAT_CHECKLIST_SET, { agentId, content });
    },
    [ws, agentId],
  );

  const fetchTargets = useCallback(async (): Promise<DeliveryTarget[]> => {
    if (!agentId || !ws.isConnected) return [];
    const res = await ws.call<{ targets: DeliveryTarget[] }>(
      Methods.HEARTBEAT_TARGETS, { agentId },
    );
    return res.targets ?? [];
  }, [ws, agentId]);

  return {
    config,
    loading,
    saving,
    error,
    toggle,
    update,
    test,
    fetchLogs,
    getChecklist,
    setChecklist,
    fetchTargets,
    refresh,
  };
}

