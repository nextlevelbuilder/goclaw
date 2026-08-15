import { useCallback, useEffect, useMemo, useState } from "react";
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
    if (opts.limit !== undefined) p.limit = String(opts.limit);
    if (opts.offset !== undefined) p.offset = String(opts.offset);
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

/** Tenant-wide vault search (agent_id optional). */
export function useVaultSearchAll() {
  const http = useHttp();

  const search = useCallback(
    async (query: string, opts?: { agentId?: string; docTypes?: string[]; teamId?: string; maxResults?: number }) => {
      return http.post<VaultSearchResult[]>("/v1/vault/search", {
        query,
        agent_id: opts?.agentId || undefined,
        doc_types: opts?.docTypes,
        team_id: opts?.teamId || undefined,
        max_results: opts?.maxResults ?? 20,
      });
    },
    [http],
  );

  return { search };
}

/** Fetch all links for a set of vault documents (single batch query). */
export function useVaultAllLinks(agentId: string, documents: { id: string }[]) {
  const http = useHttp();
  const docIds = useMemo(() => documents.map((d) => d.id), [documents]);

  const { data, isLoading } = useQuery({
    queryKey: [VAULT_KEY, "all-links", agentId, [...docIds].sort().join(",")],
    queryFn: () => http.post<VaultLink[]>("/v1/vault/links/batch", { doc_ids: docIds }),
    enabled: docIds.length > 0,
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

  // Fetch all links in one batch query (single SQL, no N+1).
  const docIds = useMemo(() => documents.map((d) => d.id), [documents]);
  const { data: linksData, isLoading: linksLoading } = useQuery({
    queryKey: [VAULT_KEY, "graph-links", docIdKey],
    queryFn: () => http.post<VaultLink[]>("/v1/vault/links/batch", { doc_ids: docIds }),
    enabled: documents.length > 0,
    staleTime: 60_000,
  });

  return { documents, links: linksData ?? [], loading: docsLoading || linksLoading };
}

/** Enriched backlink with source doc metadata (from backend JOIN). */
export interface VaultBacklink {
  from_doc_id: string;
  context: string;
  title: string;
  path: string;
  team_id?: string;
}

/** Get links (outlinks + backlinks) for a vault document. */
export function useVaultLinks(docId: string | null) {
  const http = useHttp();

  const { data, isLoading } = useQuery({
    queryKey: [VAULT_KEY, "links", docId],
    queryFn: () => http.get<{
      outlinks: VaultLink[];
      backlinks: VaultBacklink[];
      doc_names: Record<string, string>;
    }>(`/v1/vault/documents/${docId}/links`),
    enabled: !!docId,
    placeholderData: (prev) => prev,
  });

  return {
    outlinks: data?.outlinks ?? [],
    backlinks: data?.backlinks ?? [],
    docNames: data?.doc_names ?? {},
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

/** Fetch an image file as blob URL for authenticated rendering in <img> tags. */
export function useVaultImageUrl(path: string | null): { url: string | null; error: boolean } {
  const http = useHttp();
  const [url, setUrl] = useState<string | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    if (!path) { setUrl(null); setError(false); return; }
    let revoke: string | null = null;
    setError(false);
    http.downloadBlob(`/v1/storage/files/${encodeURIComponent(path)}?raw=true`)
      .then((blob) => { revoke = URL.createObjectURL(blob); setUrl(revoke); })
      .catch(() => { setUrl(null); setError(true); });
    return () => { if (revoke) URL.revokeObjectURL(revoke); };
  }, [path, http]);

  return { url, error };
}

// Re-export mutations for convenience — consumers can import from this single file
export {
  useCreateDocument,
  useUpdateDocument,
  useDeleteDocument,
  useCreateLink,
  useDeleteLink,
  useRescanWorkspace,
  useStopEnrichment,
} from "./use-vault-mutations";
