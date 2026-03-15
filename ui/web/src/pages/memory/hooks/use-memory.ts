import { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import type {
  MemoryDocument,
  MemoryDocumentDetail,
  MemoryChunk,
  MemorySearchResult,
} from "@/types/memory";

export interface MemoryDocFilters {
  agentId?: string;
  userId?: string;
}

export function useMemoryDocuments(filters: MemoryDocFilters) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryKey = queryKeys.memory.list({ ...filters });

  const { data, isLoading, isFetching } = useQuery({
    queryKey,
    queryFn: async () => {
      // No agent selected → list all memory across all agents
      if (!filters.agentId) {
        const res = await http.get<MemoryDocument[]>("/v1/memory/documents");
        return res ?? [];
      }
      const params: Record<string, string> = {};
      if (filters.userId) params.user_id = filters.userId;
      const res = await http.get<MemoryDocument[]>(
        `/v1/agents/${filters.agentId}/memory/documents`,
        params,
      );
      return res ?? [];
    },
    placeholderData: (prev) => prev,
  });

  const documents = data ?? [];

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.memory.all }),
    [queryClient],
  );

  const getDocument = useCallback(
    async (path: string, userId?: string) => {
      const params: Record<string, string> = {};
      if (userId) params.user_id = userId;
      return http.get<MemoryDocumentDetail>(
        `/v1/agents/${filters.agentId}/memory/documents/${path}`,
        params,
      );
    },
    [http, filters.agentId],
  );

  const createDocument = useCallback(
    async (path: string, content: string, userId?: string) => {
      try {
        await http.put(`/v1/agents/${filters.agentId}/memory/documents/${path}`, {
          content,
          user_id: userId || "",
        });
        await invalidate();
        toast.success(i18n.t("memory:toast.docCreated"), path);
      } catch (err) {
        toast.error(i18n.t("memory:toast.docCreateFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        throw err;
      }
    },
    [http, filters.agentId, invalidate],
  );

  const updateDocument = useCallback(
    async (path: string, content: string, userId?: string) => {
      try {
        await http.put(`/v1/agents/${filters.agentId}/memory/documents/${path}`, {
          content,
          user_id: userId || "",
        });
        await invalidate();
        toast.success(i18n.t("memory:toast.docUpdated"), path);
      } catch (err) {
        toast.error(i18n.t("memory:toast.docUpdateFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        throw err;
      }
    },
    [http, filters.agentId, invalidate],
  );

  const deleteDocument = useCallback(
    async (path: string, userId?: string) => {
      try {
        const qs = userId ? `?user_id=${encodeURIComponent(userId)}` : "";
        await http.delete(`/v1/agents/${filters.agentId}/memory/documents/${path}${qs}`);
        await invalidate();
        toast.success(i18n.t("memory:toast.docDeleted"), path);
      } catch (err) {
        toast.error(i18n.t("memory:toast.docDeleteFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        throw err;
      }
    },
    [http, filters.agentId, invalidate],
  );

  const getChunks = useCallback(
    async (path: string, userId?: string) => {
      const params: Record<string, string> = { path };
      if (userId) params.user_id = userId;
      return http.get<MemoryChunk[]>(
        `/v1/agents/${filters.agentId}/memory/chunks`,
        params,
      );
    },
    [http, filters.agentId],
  );

  const indexDocument = useCallback(
    async (path: string, userId?: string) => {
      try {
        await http.post(`/v1/agents/${filters.agentId}/memory/index`, {
          path,
          user_id: userId || "",
        });
        toast.success(i18n.t("memory:toast.docIndexed"), path);
      } catch (err) {
        toast.error(i18n.t("memory:toast.docIndexFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        throw err;
      }
    },
    [http, filters.agentId],
  );

  const indexAll = useCallback(
    async (userId?: string) => {
      try {
        await http.post(`/v1/agents/${filters.agentId}/memory/index-all`, {
          user_id: userId || "",
        });
        toast.success(i18n.t("memory:toast.allIndexed"));
      } catch (err) {
        toast.error(i18n.t("memory:toast.allIndexFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        throw err;
      }
    },
    [http, filters.agentId],
  );

  return {
    documents,
    loading: isLoading,
    fetching: isFetching,
    refresh: invalidate,
    getDocument,
    createDocument,
    updateDocument,
    deleteDocument,
    getChunks,
    indexDocument,
    indexAll,
  };
}

export function useMemorySearch(agentId: string) {
  const http = useHttp();
  const [results, setResults] = useState<MemorySearchResult[]>([]);
  const [searching, setSearching] = useState(false);

  const search = useCallback(
    async (query: string, userId?: string, maxResults?: number, minScore?: number) => {
      setSearching(true);
      try {
        const res = await http.post<{ results: MemorySearchResult[]; count: number }>(
          `/v1/agents/${agentId}/memory/search`,
          {
            query,
            user_id: userId || "",
            max_results: maxResults || 10,
            min_score: minScore || 0,
          },
        );
        setResults(res.results ?? []);
        return res.results ?? [];
      } catch (err) {
        toast.error(i18n.t("memory:toast.searchFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        setResults([]);
        return [];
      } finally {
        setSearching(false);
      }
    },
    [http, agentId],
  );

  return { results, searching, search };
}
