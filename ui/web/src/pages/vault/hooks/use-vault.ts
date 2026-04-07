import { useCallback, useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import type { VaultDocument, VaultLink, VaultSearchResult } from "@/types/vault";

const VAULT_KEY = "vault";

/** List vault documents — cross-agent (agentId empty) or per-agent. */
export function useVaultDocuments(agentId: string, opts: { scope?: string; docType?: string; limit?: number; offset?: number }) {
  const http = useHttp();

  const params = useMemo(() => {
    const p: Record<string, string> = {};
    if (agentId) p.agent_id = agentId;
    if (opts.scope) p.scope = opts.scope;
    if (opts.docType) p.doc_type = opts.docType;
    if (opts.limit) p.limit = String(opts.limit);
    if (opts.offset) p.offset = String(opts.offset);
    return p;
  }, [agentId, opts.scope, opts.docType, opts.limit, opts.offset]);

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [VAULT_KEY, "docs", params],
    queryFn: () => http.get<VaultDocument[]>("/v1/vault/documents", params),
    placeholderData: (prev) => prev,
  });

  return { documents: data ?? [], loading: isLoading, fetching: isFetching };
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
    queryKey: [VAULT_KEY, "all-links", agentId, docIds.length],
    queryFn: async () => {
      if (docIds.length === 0) return [];
      // Fetch outlinks for all docs in parallel (batch of 10)
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
      // Deduplicate by link id
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

/** Get links (outlinks + backlinks) for a vault document. */
export function useVaultLinks(agentId: string, docId: string | null) {
  const http = useHttp();

  const { data, isLoading } = useQuery({
    queryKey: [VAULT_KEY, "links", agentId, docId],
    queryFn: () => http.get<{ outlinks: VaultLink[]; backlinks: VaultLink[] }>(
      `/v1/agents/${agentId}/vault/documents/${docId}/links`,
    ),
    enabled: !!docId && !!agentId,
  });

  return {
    outlinks: data?.outlinks ?? [],
    backlinks: data?.backlinks ?? [],
    loading: isLoading,
  };
}
