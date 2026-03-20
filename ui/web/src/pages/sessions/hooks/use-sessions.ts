import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import type { SessionInfo, SessionPreview, Message } from "@/types/session";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";

interface UseSessionsOptions {
  agentFilter?: string;
  limit?: number;
  offset?: number;
}

export function useSessions(opts: UseSessionsOptions = {}) {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const queryClient = useQueryClient();
  const { agentFilter, limit, offset } = opts;

  const queryKey = queryKeys.sessions.list({ agentFilter, limit, offset });

  const { data, isPending: loading } = useQuery({
    queryKey,
    queryFn: async () => {
      if (!ws.isConnected) return { sessions: [] as SessionInfo[], total: 0 };
      const res = await ws.call<{ sessions: SessionInfo[]; total?: number }>(Methods.SESSIONS_LIST, {
        agentId: agentFilter || undefined,
        limit,
        offset,
      });
      return { sessions: res.sessions ?? [], total: res.total ?? 0 };
    },
    placeholderData: (prev) => prev,
    enabled: connected,
  });

  const sessions = data?.sessions ?? [];
  const total = data?.total ?? 0;

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.sessions.all }),
    [queryClient],
  );

  const preview = useCallback(
    async (key: string): Promise<SessionPreview | null> => {
      if (!ws.isConnected) return null;
      const res = await ws.call<{ key: string; messages: Message[]; summary?: string }>(
        Methods.SESSIONS_PREVIEW,
        { key },
      );
      return { key: res.key, messages: res.messages ?? [], summary: res.summary };
    },
    [ws],
  );

  const deleteSession = useCallback(
    async (key: string) => {
      if (!ws.isConnected) return;
      try {
        await ws.call(Methods.SESSIONS_DELETE, { key });
        await invalidate();
        toast.success(i18next.t("sessions:toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("sessions:toast.deleteFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [ws, invalidate],
  );

  const resetSession = useCallback(
    async (key: string) => {
      if (!ws.isConnected) return;
      try {
        await ws.call(Methods.SESSIONS_RESET, { key });
        await invalidate();
        toast.success(i18next.t("sessions:toast.reset"));
      } catch (err) {
        toast.error(i18next.t("sessions:toast.resetFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [ws, invalidate],
  );

  const patchSession = useCallback(
    async (key: string, updates: { label?: string; model?: string; metadata?: Record<string, string> }) => {
      if (!ws.isConnected) return;
      try {
        await ws.call(Methods.SESSIONS_PATCH, { key, ...updates });
        await invalidate();
        toast.success(i18next.t("sessions:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("sessions:toast.updateFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [ws, invalidate],
  );

  return { sessions, total, loading, refresh: invalidate, preview, deleteSession, resetSession, patchSession };
}
