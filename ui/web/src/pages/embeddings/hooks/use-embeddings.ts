import { useState, useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";

export interface EmbeddingChunk {
  id: string;
  agent_id: string;
  graph_id: string;
  chat_id: string;
  chat_name: string;
  sender: string;
  sender_id: string;
  msg_time_from: string;
  msg_time_to: string;
  chunk_index: number;
  text: string;
  content_hash: string;
  has_embedding: boolean;
  source_msg_ids: string[];
  created_at: string;
}

interface EmbeddingsResponse {
  chunks: EmbeddingChunk[];
  total: number;
  limit: number;
  offset: number;
}

interface DeleteResponse {
  deleted_count: number;
}

export function useEmbeddings() {
  const http = useHttp();
  const [chunks, setChunks] = useState<EmbeddingChunk[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  const loadChunks = useCallback(
    async (params?: {
      agentId?: string;
      chatId?: string;
      graphId?: string;
      sender?: string;
      hasEmbedding?: boolean;
      fromTime?: string;
      toTime?: string;
      limit?: number;
      offset?: number;
    }) => {
      setLoading(true);
      try {
        const query: Record<string, string> = {};
        if (params?.agentId) query.agent_id = params.agentId;
        if (params?.chatId) query.chat_id = params.chatId;
        if (params?.graphId) query.graph_id = params.graphId;
        if (params?.sender) query.sender = params.sender;
        if (params?.hasEmbedding !== undefined) query.has_embedding = String(params.hasEmbedding);
        if (params?.fromTime) query.from_time = params.fromTime;
        if (params?.toTime) query.to_time = params.toTime;
        if (params?.limit) query.limit = String(params.limit);
        if (params?.offset) query.offset = String(params.offset);
        const res = await http.get<EmbeddingsResponse>("/v1/embeddings", query);
        setChunks(res?.chunks ?? []);
        setTotal(res?.total ?? 0);
      } catch {
        // ignore
      } finally {
        setLoading(false);
      }
    },
    [http],
  );

  const deleteChunks = useCallback(
    async (ids: string[]): Promise<number> => {
      const res = await http.post<DeleteResponse>("/v1/embeddings/delete", { ids });
      return res?.deleted_count ?? 0;
    },
    [http],
  );

  const deleteByChat = useCallback(
    async (agentId: string, chatId: string): Promise<number> => {
      const res = await http.post<DeleteResponse>("/v1/embeddings/delete-by-chat", {
        agent_id: agentId,
        chat_id: chatId,
      });
      return res?.deleted_count ?? 0;
    },
    [http],
  );

  return { chunks, total, loading, loadChunks, deleteChunks, deleteByChat };
}
