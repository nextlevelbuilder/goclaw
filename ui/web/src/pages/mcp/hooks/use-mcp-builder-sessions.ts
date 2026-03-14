import { useMemo, useCallback } from "react";
import { useChatSessions } from "@/pages/chat/hooks/use-chat-sessions";
import { parseSessionKey } from "@/lib/session-key";

const BUILDER_AGENT_ID = "default";

export function useMCPBuilderSessions() {
  const { sessions, loading, error, refresh } =
    useChatSessions(BUILDER_AGENT_ID);

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
      `agent:${BUILDER_AGENT_ID}:mcp-builder-${userId}-${Date.now().toString(36)}`,
    [],
  );

  return {
    sessions: filtered,
    loading,
    error,
    refresh,
    buildNewBuilderSessionKey,
  };
}
