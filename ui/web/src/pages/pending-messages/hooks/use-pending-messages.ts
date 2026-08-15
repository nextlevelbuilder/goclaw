import { useState, useCallback } from "react";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import type { PendingMessageGroup, PendingMessage } from "../types";

export function usePendingMessages() {
  const http = useHttp();
  const [groups, setGroups] = useState<PendingMessageGroup[]>([]);
  const [messages, setMessages] = useState<PendingMessage[]>([]);
  const [loading, setLoading] = useState(false);
  const [messagesLoading, setMessagesLoading] = useState(false);

  const loadGroups = useCallback(async () => {
    setLoading(true);
    try {
      const res = await http.get<{ groups: PendingMessageGroup[] }>("/v1/pending-messages");
      setGroups(res?.groups ?? []);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [http]);

  const loadMessages = useCallback(
    async (channel: string, key: string) => {
      setMessagesLoading(true);
      try {
        const res = await http.get<{ messages: PendingMessage[] }>("/v1/pending-messages/messages", {
          channel,
          key,
        });
        setMessages(res?.messages ?? []);
      } catch {
        // ignore
      } finally {
        setMessagesLoading(false);
      }
    },
    [http],
  );

  const compactGroup = useCallback(
    async (channel: string, key: string) => {
      try {
        const res = await http.post<{ status: string; method?: string; remaining?: number }>(
          "/v1/pending-messages/compact",
          { channel_name: channel, history_key: key },
        );
        const method = res?.method ?? "summarizing";
        if (method === "deleted") {
          toast.success(
            i18next.t("pending-messages:toast.compacted"),
            i18next.t("pending-messages:toast.compactedDeleted"),
          );
        } else {
          // Backend runs LLM in background — show info toast and let caller poll
          toast.info(
            i18next.t("pending-messages:toast.compacting"),
            i18next.t("pending-messages:toast.compactingDesc"),
          );
        }
        return true;
      } catch (err) {
        toast.error(
          i18next.t("pending-messages:toast.failedCompact"),
          err instanceof Error ? err.message : "",
        );
        return false;
      }
    },
    [http],
  );

  const clearGroup = useCallback(
    async (channel: string, key: string) => {
      try {
        await http.delete(`/v1/pending-messages?channel=${encodeURIComponent(channel)}&key=${encodeURIComponent(key)}`);
        toast.success(i18next.t("pending-messages:toast.cleared"));
        return true;
      } catch (err) {
        toast.error(
          i18next.t("pending-messages:toast.failedClear"),
          err instanceof Error ? err.message : "",
        );
        return false;
      }
    },
    [http],
  );

  return {
    groups,
    messages,
    loading,
    messagesLoading,
    loadGroups,
    loadMessages,
    compactGroup,
    clearGroup,
  };
}
