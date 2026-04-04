import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Search, Database } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { PageHeader } from "@/components/shared/page-header";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { formatUserLabel } from "@/lib/format-user-label";
import { useMemoryDocuments } from "./hooks/use-memory";
import { MemoryDocumentDialog } from "./memory-document-dialog";
import { MemoryCreateDialog } from "./memory-create-dialog";
import { MemorySearchDialog } from "./memory-search-dialog";
import { MemoryDocumentsTable } from "./memory-documents-table";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useEmbeddingStatus } from "@/hooks/use-embedding-status";
import type { MemoryDocument } from "@/types/memory";

export function MemoryPage() {
  const { t } = useTranslation("memory");
  const { t: tc } = useTranslation("common");
  const { t: to } = useTranslation("overview");
  const { agents } = useAgents();
  const { status: embStatus } = useEmbeddingStatus();
  const [agentId, setAgentId] = useState("");
  const [userIdFilter, setUserIdFilter] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [viewDoc, setViewDoc] = useState<MemoryDocument | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<MemoryDocument | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [indexAllLoading, setIndexAllLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const {
    documents,
    loading,
    fetching,
    refresh,
    deleteDocument,
    indexDocument,
    indexAll,
  } = useMemoryDocuments({ agentId: agentId || undefined, userId: userIdFilter || undefined });

  const spinning = useMinLoading(fetching);
  const showSkeleton = useDeferredLoading(loading && documents.length === 0);

  // Extract unique user_ids from documents for the scope filter dropdown
  const userIds = useMemo(() => {
    const set = new Set<string>();
    for (const doc of documents) {
      if (doc.user_id) set.add(doc.user_id);
    }
    return Array.from(set).sort();
  }, [documents]);

  // Resolve user IDs to display names via contacts API
  const { resolve: resolveContact } = useContactResolver(userIds);

  // Build agent lookup map for displaying agent names in global view
  const agentMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) {
      map.set(a.id, a.display_name || a.agent_key);
    }
    return map;
  }, [agents]);

  const selectedAgent = agents.find((a) => a.id === agentId);

  // Client-side pagination
  const total = documents.length;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const paginatedDocs = useMemo(() => {
    const start = (page - 1) * pageSize;
    return documents.slice(start, start + pageSize);
  }, [documents, page, pageSize]);

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await deleteDocument(deleteTarget.path, deleteTarget.user_id, deleteTarget.agent_id);
      setDeleteTarget(null);
    } finally {
      setDeleteLoading(false);
    }
  };

  const handleIndexAll = async () => {
    setIndexAllLoading(true);
    try {
      await indexAll(userIdFilter || undefined);
    } finally {
      setIndexAllLoading(false);
    }
  };

  const handleReindex = async (doc: MemoryDocument) => {
    await indexDocument(doc.path, doc.user_id);
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={
          <span className="flex items-center gap-2 flex-wrap">
            {t("description")}
            {embStatus && (
              <Badge variant={embStatus.configured ? "outline" : "secondary"} className="text-xs font-normal">
                {embStatus.configured ? `${to("embedding.title")}: ${embStatus.model}` : `${to("embedding.title")}: ${to("embedding.notConfigured")}`}
              </Badge>
            )}
          </span>
        }
        actions={
          <div className="flex gap-2">
            {agentId && (
              <>
                <Button size="sm" onClick={() => setSearchOpen(true)} className="gap-1" variant="outline">
                  <Search className="h-3.5 w-3.5" /> {t("search")}
                </Button>
                <Button size="sm" onClick={() => setCreateOpen(true)} className="gap-1">
                  <Plus className="h-3.5 w-3.5" /> {t("create")}
                </Button>
              </>
            )}
            <Button variant="outline" size="sm" onClick={() => refresh()} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      {/* Filters */}
      <div className="mt-4 flex flex-wrap items-end gap-3">
        <div className="grid gap-1.5">
          <Label htmlFor="mem-agent" className="text-xs">{t("filters.agent")}</Label>
          <select
            id="mem-agent"
            value={agentId}
            onChange={(e) => { setAgentId(e.target.value); setUserIdFilter(""); setPage(1); }}
            className="h-9 rounded-md border bg-background px-3 text-base md:text-sm"
          >
            <option value="">{t("filters.allAgents")}</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.display_name || a.agent_key}
              </option>
            ))}
          </select>
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="mem-scope" className="text-xs">{t("filters.scope")}</Label>
          <select
            id="mem-scope"
            value={userIdFilter}
            onChange={(e) => { setUserIdFilter(e.target.value); setPage(1); }}
            className="h-9 rounded-md border bg-background px-3 text-base md:text-sm min-w-[180px]"
          >
            <option value="">{t("filters.allScope")}</option>
            {userIds.map((uid) => (
              <option key={uid} value={uid}>
                {formatUserLabel(uid, resolveContact)}
              </option>
            ))}
          </select>
        </div>
        {agentId && (
          <Button
            variant="outline"
            size="sm"
            onClick={handleIndexAll}
            disabled={indexAllLoading}
            className="h-9 gap-1"
          >
            <Database className="h-3.5 w-3.5" />
            {indexAllLoading ? t("indexing") : t("indexAll")}
          </Button>
        )}
      </div>

      {/* Document table */}
      <div className="mt-4">
        <MemoryDocumentsTable
          documents={documents}
          paginatedDocs={paginatedDocs}
          loading={showSkeleton}
          agentId={agentId}
          agentWorkspace={selectedAgent?.workspace}
          page={page}
          pageSize={pageSize}
          total={total}
          totalPages={totalPages}
          resolveContact={resolveContact}
          agentMap={agentMap}
          onViewDoc={setViewDoc}
          onDeleteTarget={setDeleteTarget}
          onReindex={handleReindex}
          onPageChange={setPage}
          onPageSizeChange={(size) => { setPageSize(size); setPage(1); }}
        />
      </div>

      {/* Dialogs */}
      <MemoryDocumentDialog
        open={!!viewDoc}
        onOpenChange={(open) => !open && setViewDoc(null)}
        agentId={viewDoc?.agent_id || agentId}
        document={viewDoc}
      />

      <MemoryCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        agentId={agentId || undefined}
        knownUserIds={userIds}
      />

      <MemorySearchDialog
        open={searchOpen}
        onOpenChange={setSearchOpen}
        agentId={agentId}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { path: deleteTarget?.path })}
        confirmLabel={t("delete.confirmLabel")}
        variant="destructive"
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
    </div>
  );
}

