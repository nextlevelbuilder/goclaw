import { useCallback, useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import type { VaultDocument, VaultLink, VaultSearchResult } from "@/types/vault";

export const VAULT_KEY = "vault";

interface VaultDocListResponse {
  documents: VaultDocument[];
  total: number;
}

interface VaultListOpts {
  scope?: string;
  docType?: string;
  teamId?: string;
  limit?: number;
  offset?: number;
}

/** List vault documents — cross-agent (agentId empty) or per-agent. */
export function useVaultDocuments(agentId: string, opts: VaultListOpts) {
  const http = useHttp();

  const params = useMemo(() => {
    const p: Record<string, string> = {};
    if (agentId) p.agent_id = agentId;
    if (opts.scope) p.scope = opts.scope;
    if (opts.docType) p.doc_type = opts.docType;
    if (opts.teamId) p.team_id = opts.teamId;
    if (opts.limit) p.limit = String(opts.limit);
    if (opts.offset) p.offset = String(opts.offset);
    return p;
  }, [agentId, opts.scope, opts.docType, opts.teamId, opts.limit, opts.offset]);

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [VAULT_KEY, "docs", params],
    queryFn: () => http.get<VaultDocListResponse>("/v1/vault/documents", params),
    placeholderData: (prev) => prev,
  });

  return {
    documents: data?.documents ?? [],
    total: data?.total ?? 0,
    loading: isLoading,
    fetching: isFetching,
  };
}

/** Search vault documents for a specific agent. */
export function useVaultSearch(agentId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const search = useCallback(
    async (query: string, opts?: { scope?: string; docTypes?: string[]; maxResults?: number }) => {
      const results = await http.post<VaultSearchResult[]>(`/v1/agents/${agentId}/vault/search`, {
        query,
        scope: opts?.scope,
        doc_types: opts?.docTypes,
        max_results: opts?.maxResults ?? 10,
      });
      return results;
    },
    [http, agentId],
  );

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: [VAULT_KEY] });
  }, [queryClient]);

  return { search, invalidate };
}

/** Fetch all links for a set of vault documents (for graph view). */
export function useVaultAllLinks(agentId: string, documents: { id: string }[]) {
  const http = useHttp();

  const docIds = useMemo(() => documents.map((d) => d.id), [documents]);

  const { data, isLoading } = useQuery({
    queryKey: [VAULT_KEY, "all-links", agentId, [...docIds].sort().join(",")],
    queryFn: async () => {
      if (docIds.length === 0) return [];
      const allLinks: VaultLink[] = [];
      const batchSize = 10;
      for (let i = 0; i < docIds.length; i += batchSize) {
        const batch = docIds.slice(i, i + batchSize);
        const results = await Promise.all(
          batch.map((id) =>
            http.get<{ outlinks: VaultLink[]; backlinks: VaultLink[] }>(
              `/v1/agents/${agentId}/vault/documents/${id}/links`,
            ).catch(() => ({ outlinks: [], backlinks: [] })),
          ),
        );
        for (const r of results) {
          allLinks.push(...r.outlinks);
        }
      }
      const seen = new Set<string>();
      return allLinks.filter((l) => {
        if (seen.has(l.id)) return false;
        seen.add(l.id);
        return true;
      });
    },
    enabled: !!agentId && docIds.length > 0,
    staleTime: 60_000,
  });

  return { links: data ?? [], loading: isLoading };
}

/** Independent data fetch for graph view — higher limit, separate cache.
 *  Fetches links per-agent (groups docs by agent_id) so links work in all-agents mode too. */
export function useVaultGraphData(agentId: string, opts?: { teamId?: string }) {
  const http = useHttp();

  const params = useMemo(() => {
    const p: Record<string, string> = { limit: "500" };
    if (agentId) p.agent_id = agentId;
    if (opts?.teamId) p.team_id = opts.teamId;
    return p;
  }, [agentId, opts?.teamId]);

  const { data: docData, isLoading: docsLoading } = useQuery({
    queryKey: [VAULT_KEY, "graph-docs", params],
    queryFn: () => http.get<VaultDocListResponse>("/v1/vault/documents", params),
    staleTime: 60_000,
  });

  const documents = docData?.documents ?? [];

  // Build a stable cache key from doc IDs
  const docIdKey = useMemo(
    () => documents.map((d) => d.id).sort().join(","),
    [documents],
  );

  // Fetch links grouped by agent_id — works for both single-agent and all-agents mode.
  const { data: linksData, isLoading: linksLoading } = useQuery({
    queryKey: [VAULT_KEY, "graph-links", docIdKey],
    queryFn: async () => {
      if (documents.length === 0) return [];
      // Group doc IDs by agent_id
      const byAgent = new Map<string, string[]>();
      for (const doc of documents) {
        const aid = doc.agent_id;
        if (!aid) continue;
        if (!byAgent.has(aid)) byAgent.set(aid, []);
        byAgent.get(aid)!.push(doc.id);
      }

      const allLinks: VaultLink[] = [];
      for (const [aid, ids] of byAgent) {
        const batchSize = 10;
        for (let i = 0; i < ids.length; i += batchSize) {
          const batch = ids.slice(i, i + batchSize);
          const results = await Promise.all(
            batch.map((id) =>
              http.get<{ outlinks: VaultLink[]; backlinks: VaultLink[] }>(
                `/v1/agents/${aid}/vault/documents/${id}/links`,
              ).catch(() => ({ outlinks: [], backlinks: [] })),
            ),
          );
          for (const r of results) allLinks.push(...r.outlinks);
        }
      }
      // Dedup
      const seen = new Set<string>();
      return allLinks.filter((l) => {
        if (seen.has(l.id)) return false;
        seen.add(l.id);
        return true;
      });
    },
    enabled: documents.length > 0,
    staleTime: 60_000,
  });

  return { documents, links: linksData ?? [], loading: docsLoading || linksLoading };
}

/** Get links (outlinks + backlinks) for a vault document. */
export function useVaultLinks(agentId: string, docId: string | null) {
  const http = useHttp();

  const { data, isLoading } = useQuery({
    queryKey: [VAULT_KEY, "links", agentId, docId],
    queryFn: () => http.get<{ outlinks: VaultLink[]; backlinks: VaultLink[] }>(
      `/v1/agents/${agentId}/vault/documents/${docId}/links`,
    ),
    enabled: !!docId && !!agentId,
    placeholderData: (prev) => prev,
  });

  return {
    outlinks: data?.outlinks ?? [],
    backlinks: data?.backlinks ?? [],
    loading: isLoading,
  };
}

/** Fetch file content for a vault document via storage endpoint. */
export function useVaultFileContent(path: string | null) {
  const http = useHttp();

  const { data, isLoading, error } = useQuery({
    queryKey: [VAULT_KEY, "file-content", path],
    queryFn: () => http.get<{ content: string; path: string; size: number }>(
      `/v1/storage/files/${encodeURIComponent(path!)}`,
    ),
    enabled: !!path,
    staleTime: 60_000,
    retry: false,
    placeholderData: (prev) => prev,
  });

  return { content: data?.content ?? null, size: data?.size ?? 0, loading: isLoading, error: !!error };
}

// Re-export mutations for convenience — consumers can import from this single file
export {
  useCreateDocument,
  useUpdateDocument,
  useDeleteDocument,
  useCreateLink,
  useDeleteLink,
} from "./use-vault-mutations";
