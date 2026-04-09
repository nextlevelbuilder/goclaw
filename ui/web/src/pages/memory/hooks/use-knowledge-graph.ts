import { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import type { KGEntity, KGRelation, KGStats, KGTraversalResult, KGDedupCandidate } from "@/types/knowledge-graph";

export interface KGFilters {
  agentId: string;
  userId?: string;
  entityType?: string;
  query?: string;
}

export function useKnowledgeGraph(filters: KGFilters) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryKey = queryKeys.kg.list({ ...filters });

  const { data, isLoading, isFetching } = useQuery({
    queryKey,
    queryFn: async () => {
      if (!filters.agentId) return [];
      const params: Record<string, string> = {};
      if (filters.userId) params.user_id = filters.userId;
      if (filters.entityType) params.type = filters.entityType;
      if (filters.query) params.q = filters.query;
      params.limit = "200";
      return (await http.get<KGEntity[]>(`/v1/agents/${filters.agentId}/kg/entities`, params)) ?? [];
    },
    staleTime: 60_000,
    enabled: !!filters.agentId,
  });

  const entities = data ?? [];

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.kg.all }),
    [queryClient],
  );

  const getEntityWithRelations = useCallback(
    async (entityId: string, userId?: string) => {
      const params: Record<string, string> = {};
      if (userId) params.user_id = userId;
      return http.get<{ entity: KGEntity; relations: KGRelation[] }>(
        `/v1/agents/${filters.agentId}/kg/entities/${entityId}`,
        params,
      );
    },
    [http, filters.agentId],
  );

  const upsertEntity = useCallback(
    async (entity: Partial<KGEntity>) => {
      try {
        await http.post(`/v1/agents/${filters.agentId}/kg/entities`, entity);
        await invalidate();
        toast.success(i18n.t("memory:toast.entitySaved"), entity.name || "");
      } catch (err) {
        toast.error(i18n.t("memory:toast.entitySaveFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        throw err;
      }
    },
    [http, filters.agentId, invalidate],
  );

  const deleteEntity = useCallback(
    async (entityId: string, userId?: string) => {
      try {
        const qs = userId ? `?user_id=${encodeURIComponent(userId)}` : "";
        await http.delete(`/v1/agents/${filters.agentId}/kg/entities/${entityId}${qs}`);
        await invalidate();
        toast.success(i18n.t("memory:toast.entityDeleted"));
      } catch (err) {
        toast.error(i18n.t("memory:toast.entityDeleteFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        throw err;
      }
    },
    [http, filters.agentId, invalidate],
  );

  const extractFromText = useCallback(
    async (text: string, provider: string, model: string, userId?: string) => {
      try {
        const res = await http.post<{ entities: number; relations: number }>(
          `/v1/agents/${filters.agentId}/kg/extract`,
          { text, provider, model, user_id: userId || "" },
        );
        await invalidate();
        toast.success(i18n.t("memory:toast.extractionComplete"), `${res.entities} entities, ${res.relations} relations`);
        return res;
      } catch (err) {
        toast.error(i18n.t("memory:toast.extractionFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        throw err;
      }
    },
    [http, filters.agentId, invalidate],
  );

  return {
    entities,
    loading: isLoading,
    fetching: isFetching,
    refresh: invalidate,
    getEntityWithRelations,
    upsertEntity,
    deleteEntity,
    extractFromText,
  };
}

export function useKGStats(agentId: string, userId?: string) {
  const http = useHttp();

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.kg.stats(agentId, userId),
    queryFn: async () => {
      const params: Record<string, string> = {};
      if (userId) params.user_id = userId;
      return http.get<KGStats>(`/v1/agents/${agentId}/kg/stats`, params);
    },
    staleTime: 60_000,
    enabled: !!agentId,
    placeholderData: (prev) => prev,
  });

  return { stats: data, loading: isLoading };
}

export interface KGGraphData {
  entities: KGEntity[];
  relations: KGRelation[];
}

export function useKGGraph(agentId: string, userId?: string) {
  const http = useHttp();

  const { data, isLoading, isFetching } = useQuery({
    queryKey: queryKeys.kg.graph(agentId, userId),
    queryFn: async () => {
      if (!agentId) return { entities: [], relations: [] };
      const params: Record<string, string> = { limit: "500" };
      if (userId) params.user_id = userId;
      return http.get<KGGraphData>(`/v1/agents/${agentId}/kg/graph`, params);
    },
    staleTime: 60_000,
    enabled: !!agentId,
  });

  return {
    entities: data?.entities ?? [],
    relations: data?.relations ?? [],
    loading: isLoading,
    fetching: isFetching,
  };
}

export function useKGDedup(agentId: string, userId?: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryKey = queryKeys.kg.dedup(agentId, userId);

  const { data, isLoading, isFetching } = useQuery({
    queryKey,
    queryFn: async () => {
      if (!agentId) return [];
      const params: Record<string, string> = { limit: "50" };
      if (userId) params.user_id = userId;
      return (await http.get<KGDedupCandidate[]>(`/v1/agents/${agentId}/kg/dedup`, params)) ?? [];
    },
    staleTime: 60_000,
    enabled: !!agentId,
    placeholderData: (prev) => prev,
  });

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey });
    queryClient.invalidateQueries({ queryKey: queryKeys.kg.all });
  }, [queryClient, queryKey]);

  const merge = useCallback(
    async (targetId: string, sourceId: string) => {
      try {
        await http.post(`/v1/agents/${agentId}/kg/merge`, {
          target_id: targetId,
          source_id: sourceId,
          user_id: userId || "",
        });
        await invalidate();
        toast.success(i18n.t("memory:kg.dedup.merged"));
      } catch (err) {
        toast.error(i18n.t("memory:kg.dedup.mergeFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, agentId, userId, invalidate],
  );

  const dismiss = useCallback(
    async (candidateId: string) => {
      try {
        await http.post(`/v1/agents/${agentId}/kg/dedup/dismiss`, { candidate_id: candidateId });
        await invalidate();
        toast.success(i18n.t("memory:kg.dedup.dismissed"));
      } catch (err) {
        toast.error(i18n.t("memory:kg.dedup.dismissFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, agentId, invalidate],
  );

  const scan = useCallback(
    async () => {
      try {
        const res = await http.post<{ candidates_found: number }>(
          `/v1/agents/${agentId}/kg/dedup/scan`,
          { user_id: userId || "", threshold: 0.90, limit: 100 },
        );
        await invalidate();
        toast.success(i18n.t("memory:kg.dedup.scanComplete"), `${res.candidates_found} candidates`);
        return res.candidates_found;
      } catch (err) {
        toast.error(i18n.t("memory:kg.dedup.scanFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, agentId, userId, invalidate],
  );

  return {
    candidates: data ?? [],
    loading: isLoading,
    fetching: isFetching,
    refresh: invalidate,
    scan,
    merge,
    dismiss,
  };
}

export function useKGTraversal(agentId: string) {
  const http = useHttp();
  const [results, setResults] = useState<KGTraversalResult[]>([]);
  const [traversing, setTraversing] = useState(false);

  const traverse = useCallback(
    async (entityId: string, userId?: string, maxDepth?: number) => {
      setTraversing(true);
      try {
        const res = await http.post<KGTraversalResult[]>(`/v1/agents/${agentId}/kg/traverse`, {
          entity_id: entityId,
          user_id: userId || "",
          max_depth: maxDepth || 2,
        });
        setResults(res ?? []);
        return res ?? [];
      } catch (err) {
        toast.error(i18n.t("memory:toast.traversalFailed"), err instanceof Error ? err.message : i18n.t("memory:toast.unknownError"));
        setResults([]);
        return [];
      } finally {
        setTraversing(false);
      }
    },
    [http, agentId],
  );

  const reset = useCallback(() => setResults([]), []);

  return { results, traversing, traverse, reset };
}
