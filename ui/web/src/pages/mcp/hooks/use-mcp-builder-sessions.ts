import { useMemo, useCallback } from "react";
import { useChatSessions } from "@/pages/chat/hooks/use-chat-sessions";
import { parseSessionKey } from "@/lib/session-key";

export function useMCPBuilderSessions(agentKey: string) {
  const { sessions, loading, error, refresh } = useChatSessions(agentKey);

  const filtered = useMemo(
    () =>
      sessions.filter((s) => {
        const { scope } = parseSessionKey(s.key);
        return scope.startsWith("mcp-builder-");
      }),
    [sessions],
  );

  const buildNewBuilderSessionKey = useCallback(
    (userId: string) =>
      `agent:${agentKey}:mcp-builder-${userId}-${Date.now().toString(36)}`,
    [agentKey],
  );

  return {
    sessions: filtered,
    loading,
    error,
    refresh,
    buildNewBuilderSessionKey,
  };
}
