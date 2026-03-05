import { useState, useEffect, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import type { SessionInfo } from "@/types/session";
import { useAuthStore } from "@/stores/use-auth-store";

/**
 * Manages the session list for the chat sidebar.
 * Loads sessions for the selected agent, supports creating new sessions.
 */
export function useChatSessions(agentId: string) {
  const ws = useWs();
  const userId = useAuthStore((s) => s.userId);
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadSessions = useCallback(async () => {
    if (!ws.isConnected) return;
    setLoading(true);
    setError(null);
    try {
      const res = await ws.call<{ sessions: SessionInfo[] }>(
        Methods.SESSIONS_LIST,
        { agentId },
      );
      const sorted = (res.sessions ?? []).sort(
        (a: SessionInfo, b: SessionInfo) =>
          new Date(b.updated).getTime() - new Date(a.updated).getTime(),
      );
      setSessions(sorted);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load sessions");
    } finally {
      setLoading(false);
    }
  }, [ws, agentId]);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  const buildNewSessionKey = useCallback(() => {
    const ts = Date.now().toString(36);
    return `agent:${agentId}:ws-${userId}-${ts}`;
  }, [agentId, userId]);

  return {
    sessions,
    loading,
    error,
    refresh: loadSessions,
    buildNewSessionKey,
  };
}
