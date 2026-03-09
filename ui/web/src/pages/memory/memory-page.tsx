import { useState, useMemo } from "react";
import { Brain, Plus, RefreshCw, Search, Database, Trash2, RotateCw, Network } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { Pagination } from "@/components/shared/pagination";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useMemoryDocuments } from "./hooks/use-memory";
import { MemoryDocumentDialog } from "./memory-document-dialog";
import { MemoryCreateDialog } from "./memory-create-dialog";
import { MemorySearchDialog } from "./memory-search-dialog";
import { KGEntitiesTab } from "./kg-entities-tab";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import type { MemoryDocument } from "@/types/memory";

export function MemoryPage() {
  const { agents } = useAgents();
  const [agentId, setAgentId] = useState("");
  const [userIdFilter, setUserIdFilter] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [viewDoc, setViewDoc] = useState<MemoryDocument | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<MemoryDocument | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [indexAllLoading, setIndexAllLoading] = useState(false);
  const [activeTab, setActiveTab] = useState<"documents" | "kg">("documents");
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
      await deleteDocument(deleteTarget.path, deleteTarget.user_id);
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
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Memory"
        description="Browse and manage agent memory documents, chunks, and embeddings"
        actions={
          <div className="flex gap-2">
            {agentId && (
              <>
                <Button size="sm" onClick={() => setSearchOpen(true)} className="gap-1" variant="outline">
                  <Search className="h-3.5 w-3.5" /> Search
                </Button>
                <Button size="sm" onClick={() => setCreateOpen(true)} className="gap-1">
                  <Plus className="h-3.5 w-3.5" /> Create
                </Button>
              </>
            )}
            <Button variant="outline" size="sm" onClick={() => refresh()} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> Refresh
            </Button>
          </div>
        }
      />

      {/* Filters */}
      <div className="mt-4 flex flex-wrap items-end gap-3">
        <div className="grid gap-1.5">
          <Label htmlFor="mem-agent" className="text-xs">Agent</Label>
          <select
            id="mem-agent"
            value={agentId}
            onChange={(e) => { setAgentId(e.target.value); setUserIdFilter(""); setPage(1); }}
            className="h-9 rounded-md border bg-background px-3 text-sm"
          >
            <option value="">All agents</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.display_name || a.agent_key}
              </option>
            ))}
          </select>
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="mem-scope" className="text-xs">Scope</Label>
          <select
            id="mem-scope"
            value={userIdFilter}
            onChange={(e) => { setUserIdFilter(e.target.value); setPage(1); }}
            className="h-9 rounded-md border bg-background px-3 text-sm min-w-[180px]"
          >
            <option value="">All (global + personal)</option>
            {userIds.map((uid) => (
              <option key={uid} value={uid}>
                {formatScopeLabel(uid)}
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
            {indexAllLoading ? "Indexing..." : "Index All"}
          </Button>
        )}
      </div>

      {/* Tab bar (only when agent selected) */}
      {agentId && (
        <div className="mt-4 flex border-b">
          <button
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px ${activeTab === "documents" ? "border-primary text-primary" : "border-transparent text-muted-foreground hover:text-foreground"}`}
            onClick={() => setActiveTab("documents")}
          >
            <Brain className="inline h-3.5 w-3.5 mr-1.5" />Documents
          </button>
          <button
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px ${activeTab === "kg" ? "border-primary text-primary" : "border-transparent text-muted-foreground hover:text-foreground"}`}
            onClick={() => setActiveTab("kg")}
          >
            <Network className="inline h-3.5 w-3.5 mr-1.5" />Knowledge Graph
          </button>
        </div>
      )}

      {/* KG tab */}
      {agentId && activeTab === "kg" && (
        <div className="mt-4">
          <KGEntitiesTab agentId={agentId} userId={userIdFilter || undefined} />
        </div>
      )}

      {/* Document table */}
      <div className="mt-4" style={{ display: activeTab === "documents" || !agentId ? undefined : "none" }}>
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : documents.length === 0 ? (
          <EmptyState
            icon={Brain}
            title="No memory documents"
            description={agentId ? "This agent has no memory documents yet." : "No memory documents found across any agent."}
          />
        ) : (
          <div className="rounded-md border">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">Path</th>
                  {!agentId && <th className="px-4 py-3 text-left font-medium">Agent</th>}
                  <th className="px-4 py-3 text-left font-medium">Scope</th>
                  <th className="px-4 py-3 text-left font-medium">Hash</th>
                  <th className="px-4 py-3 text-left font-medium">Updated</th>
                  <th className="px-4 py-3 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {paginatedDocs.map((doc) => (
                  <tr key={`${doc.agent_id}-${doc.path}-${doc.user_id || "global"}`} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <button
                        className="flex items-start gap-2 text-left hover:underline cursor-pointer"
                        onClick={() => setViewDoc(doc)}
                      >
                        <Brain className="h-4 w-4 shrink-0 text-muted-foreground mt-0.5" />
                        <div>
                          <span className="font-mono text-xs font-medium">{doc.path}</span>
                          {selectedAgent?.workspace && (
                            <p className="font-mono text-[10px] text-muted-foreground">{selectedAgent.workspace}</p>
                          )}
                        </div>
                      </button>
                    </td>
                    {!agentId && (
                      <td className="px-4 py-3 text-xs text-muted-foreground">
                        {doc.agent_id ? (agentMap.get(doc.agent_id) || doc.agent_id.slice(0, 8)) : "-"}
                      </td>
                    )}
                    <td className="px-4 py-3">
                      <Badge variant={doc.user_id ? "secondary" : "outline"}>
                        {doc.user_id ? "Personal" : "Global"}
                      </Badge>
                      {doc.user_id && (
                        <span className="ml-1 text-xs text-muted-foreground">{formatScopeLabel(doc.user_id)}</span>
                      )}
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                      {doc.hash.slice(0, 8)}
                    </td>
                    <td className="px-4 py-3 text-xs text-muted-foreground">
                      {new Date(doc.updated_at).toLocaleString()}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button variant="ghost" size="sm" onClick={() => handleReindex(doc)} className="gap-1">
                          <RotateCw className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDeleteTarget(doc)}
                          className="gap-1 text-destructive hover:text-destructive"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <Pagination
              page={page}
              pageSize={pageSize}
              total={total}
              totalPages={totalPages}
              onPageChange={setPage}
              onPageSizeChange={(size) => { setPageSize(size); setPage(1); }}
            />
          </div>
        )}
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
        title="Delete Memory Document"
        description={`Are you sure you want to delete "${deleteTarget?.path}"? This will also delete all associated chunks and embeddings.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
    </div>
  );
}

/** Format user_id into a readable scope label (e.g. "group:telegram:-100xxx" -> "Telegram -100xxx") */
function formatScopeLabel(userId: string): string {
  if (userId.startsWith("group:")) {
    const parts = userId.split(":");
    if (parts.length >= 3) {
      const channel = parts[1]!.charAt(0).toUpperCase() + parts[1]!.slice(1);
      return `${channel} ${parts.slice(2).join(":")}`;
    }
  }
  return userId;
}
