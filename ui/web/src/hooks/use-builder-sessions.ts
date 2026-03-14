import { useMemo, useCallback } from "react";
import { useChatSessions } from "@/pages/chat/hooks/use-chat-sessions";
import { parseSessionKey } from "@/lib/session-key";

export function useBuilderSessions(agentKey: string, scopePrefix: string) {
  const { sessions, loading, error, refresh } = useChatSessions(agentKey);

  const filtered = useMemo(
    () =>
      sessions.filter((s) => {
        const { scope } = parseSessionKey(s.key);
        return scope.startsWith(scopePrefix);
      }),
    [sessions, scopePrefix],
  );

  const buildNewBuilderSessionKey = useCallback(
    (userId: string) =>
      `agent:${agentKey}:${scopePrefix}${userId}-${Date.now().toString(36)}`,
    [agentKey, scopePrefix],
  );

  return {
    sessions: filtered,
    loading,
    error,
    refresh,
    buildNewBuilderSessionKey,
  };
}
