import { useState, useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";

export interface RawMessage {
  id: string;
  channel_name: string;
  chat_id: string;
  chat_name: string;
  graph_id: string;
  sender: string;
  sender_id: string;
  body: string;
  msg_timestamp: string;
  agent_id: string;
  agent_name: string;
  processed_at: string | null;
  created_at: string;
}

interface RawMessagesResponse {
  messages: RawMessage[];
  total: number;
  limit: number;
  offset: number;
}

export function useRawMessages() {
  const http = useHttp();
  const [messages, setMessages] = useState<RawMessage[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  const loadMessages = useCallback(
    async (params?: { processed?: boolean; limit?: number; offset?: number }) => {
      setLoading(true);
      try {
        const query: Record<string, string> = {};
        if (params?.processed !== undefined) {
          query.processed = String(params.processed);
        }
        if (params?.limit) {
          query.limit = String(params.limit);
        }
        if (params?.offset) {
          query.offset = String(params.offset);
        }
        const res = await http.get<RawMessagesResponse>("/v1/listen-raw-messages", query);
        setMessages(res?.messages ?? []);
        setTotal(res?.total ?? 0);
      } catch {
        // ignore
      } finally {
        setLoading(false);
      }
    },
    [http],
  );

  return { messages, total, loading, loadMessages };
}
