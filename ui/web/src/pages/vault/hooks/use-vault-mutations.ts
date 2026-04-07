import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import type { VaultDocument, VaultLink } from "@/types/vault";

const VAULT_KEY = "vault";

/** Create a new vault document. */
export function useCreateDocument(agentId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const create = useCallback(
    async (body: { path: string; title: string; doc_type: string; scope: string; metadata?: Record<string, unknown> }) => {
      try {
        const doc = await http.post<VaultDocument>(`/v1/agents/${agentId}/vault/documents`, body);
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
export function useUpdateDocument(agentId: string, docId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const update = useCallback(
    async (body: { title?: string; doc_type?: string; scope?: string; metadata?: Record<string, unknown> }) => {
      try {
        const doc = await http.put<VaultDocument>(`/v1/agents/${agentId}/vault/documents/${docId}`, body);
        await queryClient.invalidateQueries({ queryKey: [VAULT_KEY] });
        toast.success(i18n.t("vault:toast.docUpdated"));
        return doc;
      } catch (err) {
        toast.error(i18n.t("vault:toast.docUpdateFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, agentId, docId, queryClient],
  );

  return { update };
}

/** Delete a vault document. */
export function useDeleteDocument(agentId: string, docId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const remove = useCallback(async () => {
    try {
      await http.delete(`/v1/agents/${agentId}/vault/documents/${docId}`);
      await queryClient.invalidateQueries({ queryKey: [VAULT_KEY] });
      toast.success(i18n.t("vault:toast.docDeleted"));
    } catch (err) {
      toast.error(i18n.t("vault:toast.docDeleteFailed"), err instanceof Error ? err.message : "");
      throw err;
    }
  }, [http, agentId, docId, queryClient]);

  return { remove };
}

/** Create a link between two vault documents. */
export function useCreateLink(agentId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const create = useCallback(
    async (body: { from_doc_id: string; to_doc_id: string; link_type: string; context?: string }) => {
      try {
        const link = await http.post<VaultLink>(`/v1/agents/${agentId}/vault/links`, body);
        await queryClient.invalidateQueries({ queryKey: [VAULT_KEY, "links"] });
        await queryClient.invalidateQueries({ queryKey: [VAULT_KEY, "all-links"] });
        toast.success(i18n.t("vault:toast.linkCreated"));
        return link;
      } catch (err) {
        toast.error(i18n.t("vault:toast.linkCreateFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, agentId, queryClient],
  );

  return { create };
}

/** Delete a vault link. */
export function useDeleteLink(agentId: string, linkId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const remove = useCallback(async () => {
    try {
      await http.delete(`/v1/agents/${agentId}/vault/links/${linkId}`);
      await queryClient.invalidateQueries({ queryKey: [VAULT_KEY, "links"] });
      await queryClient.invalidateQueries({ queryKey: [VAULT_KEY, "all-links"] });
      toast.success(i18n.t("vault:toast.linkDeleted"));
    } catch (err) {
      toast.error(i18n.t("vault:toast.linkDeleteFailed"), err instanceof Error ? err.message : "");
      throw err;
    }
  }, [http, agentId, linkId, queryClient]);

  return { remove };
}
