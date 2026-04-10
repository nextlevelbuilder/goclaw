import { useCallback } from "react";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import type { VaultDocument, VaultLink } from "@/types/vault";

interface RescanResult {
  scanned: number;
  new: number;
  updated: number;
  unchanged: number;
  skipped: number;
  errors: number;
  truncated: boolean;
}

const VAULT_KEY = "vault";

/** Create a new vault document. Agent-scoped or cross-agent (empty agentId). */
export function useCreateDocument(agentId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const create = useCallback(
    async (body: { path: string; title: string; doc_type: string; scope: string; team_id?: string; metadata?: Record<string, unknown> }) => {
      try {
        const url = agentId ? `/v1/agents/${agentId}/vault/documents` : `/v1/vault/documents`;
        const doc = await http.post<VaultDocument>(url, body);
        await queryClient.invalidateQueries({ queryKey: [VAULT_KEY] });
        toast.success(i18n.t("vault:toast.docCreated"));
        return doc;
      } catch (err) {
        toast.error(i18n.t("vault:toast.docCreateFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, agentId, queryClient],
  );

  return { create };
}

/** Update a vault document. */
export function useUpdateDocument(docId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const update = useCallback(
    async (body: { title?: string; doc_type?: string; scope?: string; metadata?: Record<string, unknown> }) => {
      try {
        const doc = await http.put<VaultDocument>(`/v1/vault/documents/${docId}`, body);
        await queryClient.invalidateQueries({ queryKey: [VAULT_KEY] });
        toast.success(i18n.t("vault:toast.docUpdated"));
        return doc;
      } catch (err) {
        toast.error(i18n.t("vault:toast.docUpdateFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, docId, queryClient],
  );

  return { update };
}

/** Delete a vault document. */
export function useDeleteDocument(docId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const remove = useCallback(async () => {
    try {
      await http.delete(`/v1/vault/documents/${docId}`);
      await queryClient.invalidateQueries({ queryKey: [VAULT_KEY] });
      toast.success(i18n.t("vault:toast.docDeleted"));
    } catch (err) {
      toast.error(i18n.t("vault:toast.docDeleteFailed"), err instanceof Error ? err.message : "");
      throw err;
    }
  }, [http, docId, queryClient]);

  return { remove };
}

/** Create a link between two vault documents. */
export function useCreateLink() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const create = useCallback(
    async (body: { from_doc_id: string; to_doc_id: string; link_type: string; context?: string }) => {
      try {
        const link = await http.post<VaultLink>(`/v1/vault/links`, body);
        await queryClient.invalidateQueries({ queryKey: [VAULT_KEY, "links"] });
        await queryClient.invalidateQueries({ queryKey: [VAULT_KEY, "all-links"] });
        toast.success(i18n.t("vault:toast.linkCreated"));
        return link;
      } catch (err) {
        toast.error(i18n.t("vault:toast.linkCreateFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, queryClient],
  );

  return { create };
}

/** Rescan workspace to sync vault documents from filesystem (tenant-wide). */
export function useRescanWorkspace() {
  const http = useHttp();
  const queryClient = useQueryClient();
  const [isPending, setIsPending] = useState(false);

  const rescan = useCallback(async () => {
    setIsPending(true);
    try {
      const result = await http.post<RescanResult>(`/v1/vault/rescan`, {});
      await queryClient.invalidateQueries({ queryKey: [VAULT_KEY] });

      const parts: string[] = [];
      if (result.new > 0) parts.push(i18n.t("vault:rescanNew", { count: result.new }));
      if (result.updated > 0) parts.push(i18n.t("vault:rescanUpdated", { count: result.updated }));
      if (result.unchanged > 0) parts.push(i18n.t("vault:rescanUnchanged", { count: result.unchanged }));

      const title = parts.length > 0 ? parts.join(", ") : i18n.t("vault:rescanNoFiles");
      const desc = result.truncated ? i18n.t("vault:rescanTruncated") : undefined;
      toast.success(title, desc);
      return result;
    } catch (err) {
      const status = (err as { status?: number })?.status;
      if (status === 409) {
        toast.warning(i18n.t("vault:rescanBusy"));
      } else {
        toast.error(i18n.t("vault:rescanError"), err instanceof Error ? err.message : "");
      }
      throw err;
    } finally {
      setIsPending(false);
    }
  }, [http, queryClient]);

  return { rescan, isPending };
}

/** Delete a vault link. */
export function useDeleteLink(linkId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const remove = useCallback(async () => {
    try {
      await http.delete(`/v1/vault/links/${linkId}`);
      await queryClient.invalidateQueries({ queryKey: [VAULT_KEY, "links"] });
      await queryClient.invalidateQueries({ queryKey: [VAULT_KEY, "all-links"] });
      toast.success(i18n.t("vault:toast.linkDeleted"));
    } catch (err) {
      toast.error(i18n.t("vault:toast.linkDeleteFailed"), err instanceof Error ? err.message : "");
      throw err;
    }
  }, [http, linkId, queryClient]);

  return { remove };
}
