import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useWs, useHttp } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import type { ChatMessage } from "@/types/chat";
import type { AttachedFile } from "@/components/chat/chat-input";

interface UseChatSendOptions {
  agentId: string;
  onMessageAdded: (msg: ChatMessage, sessionKey?: string) => void;
  onExpectRun: () => void;
}

interface MediaUploadResponse {
  path: string;
  mime_type: string;
  filename: string;
}

/**
 * Handles sending chat messages with optional file attachments.
 * Files are uploaded via HTTP first, then paths are passed to chat.send.
 */
export function useChatSend({
  agentId,
  onMessageAdded,
  onExpectRun,
}: UseChatSendOptions) {
  const { t } = useTranslation("chat");
  const ws = useWs();
  const http = useHttp();
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const send = useCallback(
    async (message: string, sessionKey: string, files?: AttachedFile[]) => {
      const hasMessage = message.trim().length > 0;
      const hasFiles = files && files.length > 0;
      if (!ws.isConnected) {
        setError(t("error.notConnected"));
        return;
      }
      if ((!hasMessage && !hasFiles) || !sessionKey) return;

      const trimmed = message.trim();
      setError(null);
      setSending(true);

      // Build optimistic display: show message + file names
      let displayContent = trimmed;
      if (hasFiles) {
        const fileNames = files.map((f) => f.file.name).join(", ");
        const fileLabel = `[${fileNames}]`;
        displayContent = displayContent ? `${fileLabel}\n${displayContent}` : fileLabel;
      }

      onMessageAdded({
        role: "user",
        content: displayContent,
        timestamp: Date.now(),
      }, sessionKey);

      try {
        // Upload files first, then pass path+filename to chat.send
        let mediaItems: { path: string; filename: string }[] | undefined;
        if (hasFiles) {
          const uploads = await Promise.all(
            files.map((af) => {
              const fd = new FormData();
              fd.append("file", af.file);
              return http.upload<MediaUploadResponse>("/v1/media/upload", fd);
            }),
          );
          mediaItems = uploads.map((u) => ({ path: u.path, filename: u.filename }));
        }

        // Expect a new run BEFORE the call — run.started event fires during ws.call,
        // not after it returns. For injected sends (agent already running), this is
        // a no-op since events are already being listened to.
        onExpectRun();

        const res = await ws.call<{ runId?: string; content?: string; injected?: boolean }>(
          Methods.CHAT_SEND,
          {
            agentId,
            sessionKey,
            message: trimmed,
            stream: true,
            ...(mediaItems && { media: mediaItems }),
          },
          600_000,
        );

        // If message was injected into running agent, no new run was started.
        // The running agent will naturally process the injected message.
        if (res?.injected) {
          return;
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to send message");
      } finally {
        setSending(false);
      }
    },
    [ws, http, agentId, onMessageAdded, onExpectRun],
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
