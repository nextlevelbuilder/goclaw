import { useState, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import type { ChatMessage } from "@/types/chat";

interface UseChatSendOptions {
  agentId: string;
  onMessageAdded: (msg: ChatMessage) => void;
  onExpectRun: () => void;
}

/**
 * Handles sending chat messages.
 * The sessionKey is passed directly to send() to avoid stale closures.
 */
export function useChatSend({
  agentId,
  onMessageAdded,
  onExpectRun,
}: UseChatSendOptions) {
  const ws = useWs();
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const send = useCallback(
    async (message: string, sessionKey: string) => {
      if (!ws.isConnected || !message.trim() || !sessionKey) return;

      const trimmed = message.trim();
      setError(null);
      setSending(true);

      // Add user message optimistically
      onMessageAdded({
        role: "user",
        content: trimmed,
        timestamp: Date.now(),
      });

      // Tell the event handler to capture the next run.started for this agent
      onExpectRun();

      try {
        await ws.call<{ runId: string; content: string }>(
          Methods.CHAT_SEND,
          {
            agentId,
            sessionKey,
            message: trimmed,
            stream: true,
          },
          600_000, // 10 min hard cap — actual timeout is managed by inactivity via resetTimeout
        );
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to send message");
      } finally {
        setSending(false);
      }
    },
    [ws, agentId, onMessageAdded, onExpectRun],
  );

  const abort = useCallback(
    async (sessionKey: string) => {
      if (!ws.isConnected || !sessionKey) return;
      try {
        await ws.call(Methods.CHAT_ABORT, { sessionKey });
      } catch {
        // ignore abort errors
      }
    },
    [ws],
  );

  return { send, abort, sending, error };
}
