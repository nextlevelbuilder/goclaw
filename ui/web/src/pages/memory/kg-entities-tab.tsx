import { useState } from "react";
import { Network, Trash2, Search, GitFork, Sparkles, RefreshCw, LayoutGrid, Share2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useTranslation } from "react-i18next";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useKnowledgeGraph, useKGStats, useKGGraph } from "./hooks/use-knowledge-graph";
import { KGEntityDetailDialog } from "./kg-entity-detail-dialog";
import { KGExtractDialog } from "./kg-extract-dialog";
import { KGGraphView } from "./kg-graph-view";
import type { KGEntity } from "@/types/knowledge-graph";

interface KGEntitiesTabProps {
  agentId: string;
  userId?: string;
}

type ViewMode = "table" | "graph";

export function KGEntitiesTab({ agentId, userId }: KGEntitiesTabProps) {
  const { t } = useTranslation("memory");
  const [searchQuery, setSearchQuery] = useState("");
  const [appliedQuery, setAppliedQuery] = useState("");
  const [viewEntity, setViewEntity] = useState<KGEntity | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<KGEntity | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [extractOpen, setExtractOpen] = useState(false);
  const [viewMode, setViewMode] = useState<ViewMode>("table");

  const { entities, loading, fetching, refresh, deleteEntity, getEntityWithRelations, extractFromText } = useKnowledgeGraph({
    agentId,
    userId,
    query: appliedQuery || undefined,
  });
  const { stats } = useKGStats(agentId, userId);
  const graphData = useKGGraph(agentId, userId);
  const showSkeleton = useDeferredLoading(loading && entities.length === 0);

  const handleSearch = () => setAppliedQuery(searchQuery.trim());
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") handleSearch();
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await deleteEntity(deleteTarget.id, deleteTarget.user_id);
      setDeleteTarget(null);
    } finally {
      setDeleteLoading(false);
    }
  };

  const handleExtract = (text: string, provider: string, model: string) =>
    extractFromText(text, provider, model, userId);

  return (
    <div className="flex h-full flex-col">
      {/* Search + stats + actions — single compact row */}
      <div className="flex items-center gap-2 mb-3">
        <Input
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={t("kg.search.placeholder")}
          className="max-w-[220px] h-8 text-xs"
        />
        <Button variant="outline" size="sm" onClick={handleSearch} disabled={fetching} className="gap-1 h-8 px-2.5">
          <Search className="h-3.5 w-3.5" />
        </Button>
        {appliedQuery && (
          <Button variant="ghost" size="sm" onClick={() => { setAppliedQuery(""); setSearchQuery(""); }} className="h-8 px-2 text-xs">
            {t("kg.search.clear")}
          </Button>
        )}

        {/* Inline stats */}
        {stats && (
          <div className="flex gap-2 text-[10px] text-muted-foreground ml-1">
            <span>{t("kg.stats.entities", { count: stats.entity_count })}</span>
            <span>{t("kg.stats.relations", { count: stats.relation_count })}</span>
            {Object.entries(stats.entity_types).map(([type, count]) => (
              <span key={type}>{type}: {count}</span>
            ))}
          </div>
        )}

        <div className="flex-1" />

        {/* View mode toggle */}
        <div className="flex rounded-md border">
          <Button
            variant={viewMode === "table" ? "secondary" : "ghost"}
            size="sm"
            onClick={() => setViewMode("table")}
            className="h-8 rounded-r-none px-2"
          >
            <LayoutGrid className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant={viewMode === "graph" ? "secondary" : "ghost"}
            size="sm"
            onClick={() => setViewMode("graph")}
            className="h-8 rounded-l-none px-2"
          >
            <Share2 className="h-3.5 w-3.5" />
          </Button>
        </div>

        <Button variant="outline" size="sm" onClick={() => refresh()} disabled={fetching} className="gap-1 h-8 px-2.5">
          <RefreshCw className={"h-3.5 w-3.5" + (fetching ? " animate-spin" : "")} />
        </Button>
        <Button variant="outline" size="sm" onClick={() => setExtractOpen(true)} className="gap-1 h-8 px-2.5">
          <Sparkles className="h-3.5 w-3.5" /> {t("kg.extract")}
        </Button>
      </div>

      {/* Content area */}
      <div className="min-h-0 flex-1">
      {viewMode === "graph" ? (
        <KGGraphView
          entities={graphData.entities}
          relations={graphData.relations}
          onEntityClick={setViewEntity}
        />
      ) : showSkeleton ? (
        <TableSkeleton rows={5} />
      ) : entities.length === 0 ? (
        <EmptyState
          icon={Network}
          title={t("kg.emptyTitle")}
          description={appliedQuery ? t("kg.emptySearchDescription") : t("kg.emptyDescription")}
        />
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full min-w-[600px] text-sm">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="px-4 py-3 text-left font-medium">{t("kg.columns.name")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("kg.columns.type")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("kg.columns.description")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("kg.columns.confidence")}</th>
                <th className="px-4 py-3 text-right font-medium">{t("kg.columns.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {entities.map((entity) => (
                <tr key={entity.id} className="border-b last:border-0 hover:bg-muted/30">
                  <td className="px-4 py-3">
                    <button
                      className="text-left hover:underline cursor-pointer font-medium"
                      onClick={() => setViewEntity(entity)}
                    >
                      {entity.name}
                    </button>
                    <p className="font-mono text-[10px] text-muted-foreground">{entity.external_id}</p>
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant="secondary">{entity.entity_type}</Badge>
                  </td>
                  <td className="px-4 py-3 text-xs text-muted-foreground max-w-[300px] truncate">
                    {entity.description || "-"}
                  </td>
                  <td className="px-4 py-3">
                    <ConfidenceBar value={entity.confidence} />
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button variant="ghost" size="sm" onClick={() => setViewEntity(entity)} className="gap-1">
                        <GitFork className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setDeleteTarget(entity)}
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
        </div>
      )}
      </div>

      {/* Entity detail dialog */}
      <KGEntityDetailDialog
        open={!!viewEntity}
        onOpenChange={(open) => !open && setViewEntity(null)}
        agentId={agentId}
        entity={viewEntity}
        getEntityWithRelations={getEntityWithRelations}
      />

      {/* Extract dialog */}
      <KGExtractDialog
        open={extractOpen}
        onOpenChange={setExtractOpen}
        onExtract={handleExtract}
      />

      {/* Delete confirmation */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("kg.deleteEntity.title")}
        description={t("kg.deleteEntity.description", { name: deleteTarget?.name ?? "" })}
        confirmLabel={t("kg.deleteEntity.confirmLabel")}
        variant="destructive"
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
    </div>
  );
}

function ConfidenceBar({ value }: { value: number }) {
  const pct = Math.round(value * 100);
  return (
    <div className="flex items-center gap-1">
      <div className="h-1.5 w-10 rounded-full bg-muted overflow-hidden">
        <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
      </div>
      <span className="text-[10px] text-muted-foreground">{pct}%</span>
    </div>
  );
}
