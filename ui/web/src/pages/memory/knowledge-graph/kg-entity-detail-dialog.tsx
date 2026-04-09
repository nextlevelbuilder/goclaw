import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { GitFork } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useHttp } from "@/hooks/use-ws";
import { useKGDetailStore } from "@/stores/use-kg-detail-store";
import { KGGraphView } from "./kg-graph-view";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import type { KGEntity, KGRelation } from "@/types/knowledge-graph";

interface KGEntityDetailDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
  entity: KGEntity | null;
  getEntityWithRelations: (entityId: string, userId?: string) => Promise<{ entity: KGEntity; relations: KGRelation[] }>;
}

export function KGEntityDetailDialog({ open, onOpenChange, agentId, entity, getEntityWithRelations }: KGEntityDetailDialogProps) {
  const { t } = useTranslation("memory");
  const http = useHttp();
  const [relations, setRelations] = useState<KGRelation[]>([]);
  const [loadingRels, setLoadingRels] = useState(false);

  // Dedicated store — isolated from main graph query cache
  const traversalResults = useKGDetailStore((s) => s.traversalResults);
  const traversing = useKGDetailStore((s) => s.traversing);
  const depth = useKGDetailStore((s) => s.depth);
  const setDepth = useKGDetailStore((s) => s.setDepth);
  const setTraversalResults = useKGDetailStore((s) => s.setTraversalResults);
  const setTraversing = useKGDetailStore((s) => s.setTraversing);
  const resetStore = useKGDetailStore((s) => s.reset);

  // Traverse using dedicated store (bypasses React Query cache)
  const traverse = useCallback(
    async (entityId: string, userId?: string, maxDepth?: number) => {
      setTraversing(true);
      try {
        const res = await http.post<import("@/types/knowledge-graph").KGTraversalResult[]>(
          `/v1/agents/${agentId}/kg/traverse`,
          { entity_id: entityId, user_id: userId || "", max_depth: maxDepth || depth },
        );
        setTraversalResults(res ?? []);
      } catch (err) {
        toast.error(i18n.t("memory:toast.traversalFailed"), err instanceof Error ? err.message : "");
        setTraversalResults([]);
      } finally {
        setTraversing(false);
      }
    },
    [http, agentId, depth, setTraversing, setTraversalResults],
  );

  // Reset store when entity changes
  useEffect(() => {
    setRelations([]);
    resetStore();
  }, [entity?.id, resetStore]);

  const loadRelations = useCallback(async () => {
    if (!entity) return;
    setLoadingRels(true);
    try {
      const res = await getEntityWithRelations(entity.id, entity.user_id);
      setRelations(res.relations ?? []);
    } catch {
      setRelations([]);
    } finally {
      setLoadingRels(false);
    }
  }, [entity, getEntityWithRelations]);

  useEffect(() => {
    if (open && entity) {
      loadRelations();
    }
  }, [open, entity, loadRelations]);

  const handleTraverse = useCallback(() => {
    if (!entity) return;
    traverse(entity.id, entity.user_id, depth);
  }, [entity, traverse, depth]);

  // Auto-traverse on open
  useEffect(() => {
    if (open && entity && !traversing) {
      traverse(entity.id, entity.user_id, depth);
    }
  }, [open, entity]); // eslint-disable-line react-hooks/exhaustive-deps

  // Build graph data from traversal results
  const graphData = useMemo(() => {
    if (!entity || traversalResults.length === 0) return null;
    const entities = [entity, ...traversalResults.map((r) => r.entity)];
    const graphRelations: KGRelation[] = traversalResults
      .filter((r) => r.path.length >= 2 && r.via && r.path[r.path.length - 2])
      .map((r, i) => {
        const parentId = r.path[r.path.length - 2]!;
        const isReverse = r.via.startsWith("~");
        const relType = isReverse ? r.via.slice(1) : r.via;
        return {
          id: `trav-${i}`,
          agent_id: entity.agent_id,
          source_entity_id: isReverse ? r.entity.id : parentId,
          relation_type: relType,
          target_entity_id: isReverse ? parentId : r.entity.id,
          confidence: r.entity.confidence,
          created_at: 0,
        };
      });
    return { entities, relations: graphRelations };
  }, [entity, traversalResults]);

  return (
    <Dialog open={open} onOpenChange={(v) => !traversing && onOpenChange(v)}>
      <DialogContent aria-describedby={undefined} className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span>{entity?.name}</span>
            {entity && (
              <Badge variant="secondary" className="text-xs">
                {entity.entity_type}
              </Badge>
            )}
          </DialogTitle>
        </DialogHeader>

        <div className="flex-1 min-h-0 overflow-y-auto py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4">
          {/* Entity info */}
          {entity && (
            <div className="grid grid-cols-1 gap-2 text-xs sm:grid-cols-2">
              <div>
                <span className="text-muted-foreground">{t("kg.entity.externalId")}</span>{" "}
                <span className="font-mono">{entity.external_id}</span>
              </div>
              <div>
                <span className="text-muted-foreground">{t("kg.entity.confidence")}</span>{" "}
                {Math.round(entity.confidence * 100)}%
              </div>
              {entity.description && (
                <div className="col-span-2">
                  <span className="text-muted-foreground">{t("kg.entity.description")}</span>{" "}
                  {entity.description}
                </div>
              )}
              {entity.source_id && (
                <div className="col-span-2">
                  <span className="text-muted-foreground">{t("kg.entity.source")}</span>{" "}
                  <span className="font-mono">{entity.source_id}</span>
                </div>
              )}
              {entity.properties && Object.keys(entity.properties).length > 0 && (
                <div className="col-span-2">
                  <span className="text-muted-foreground">{t("kg.entity.properties")}</span>
                  <pre className="mt-1 text-xs bg-muted/50 rounded p-2 whitespace-pre-wrap">{JSON.stringify(entity.properties, null, 2)}</pre>
                </div>
              )}
            </div>
          )}

          {/* Relations tabs */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <h4 className="text-sm font-medium">{t("kg.entity.relations")}</h4>
              <div className="flex items-center gap-2">
                <select
                  value={depth}
                  onChange={(e) => setDepth(Number(e.target.value))}
                  className="h-8 rounded-md border bg-background px-2 text-base md:text-sm"
                >
                  {[2, 3, 4, 5].map((d) => (
                    <option key={d} value={d}>
                      {t("kg.entity.depthOption", { depth: d })}
                    </option>
                  ))}
                </select>
                <Button variant="outline" size="sm" onClick={handleTraverse} disabled={traversing} className="gap-1">
                  <GitFork className="h-3.5 w-3.5" />
                  {traversing ? t("kg.entity.traversing") : t("kg.entity.traverse")}
                </Button>
              </div>
            </div>

            <Tabs defaultValue="table">
              <TabsList className="mb-2">
                <TabsTrigger value="table">{t("kg.entity.tabs.table")}</TabsTrigger>
                <TabsTrigger value="graph">{t("kg.entity.tabs.graph")}</TabsTrigger>
              </TabsList>

              <TabsContent value="table" className="space-y-3">
                {loadingRels ? (
                  <p className="text-xs text-muted-foreground">{t("kg.entity.loading")}</p>
                ) : relations.length === 0 ? (
                  <p className="text-xs text-muted-foreground">{t("kg.entity.noRelations")}</p>
                ) : (
                  <RelationsTable relations={relations} entityId={entity?.id} t={t} />
                )}

                {/* Traversal results */}
                {traversalResults.length > 0 && (
                  <div>
                    <h4 className="text-sm font-medium mb-2">
                      {t("kg.entity.traversalResults", { count: traversalResults.length })}
                    </h4>
                    <div className="space-y-1">
                      {traversalResults.map((tr, i) => (
                        <div key={i} className="flex items-center gap-2 text-xs rounded border p-2">
                          <Badge variant="outline" className="text-2xs">depth {tr.depth}</Badge>
                          {tr.via && (
                            tr.via.startsWith("~")
                              ? <span className="font-mono text-muted-foreground">←[{tr.via.slice(1)}]—</span>
                              : <span className="font-mono text-muted-foreground">—[{tr.via}]→</span>
                          )}
                          <span className="font-medium">{tr.entity.name}</span>
                          <Badge variant="secondary" className="text-2xs">{tr.entity.entity_type}</Badge>
                          {tr.entity.description && (
                            <span className="text-muted-foreground truncate max-w-[200px]">{tr.entity.description}</span>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </TabsContent>

              <TabsContent value="graph">
                {traversing ? (
                  <div className="flex items-center justify-center h-[400px] text-sm text-muted-foreground">
                    {t("kg.entity.traversing")}...
                  </div>
                ) : graphData ? (
                  <div className="h-[400px]">
                    <KGGraphView entities={graphData.entities} relations={graphData.relations} />
                  </div>
                ) : (
                  <div className="flex items-center justify-center h-[400px] text-sm text-muted-foreground">
                    {t("kg.entity.noRelations")}
                  </div>
                )}
              </TabsContent>
            </Tabs>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** Memoized relations table — prevents re-render when dialog parent updates */
const RelationsTable = React.memo(function RelationsTable({
  relations,
  entityId,
  t,
}: {
  relations: KGRelation[];
  entityId: string | undefined;
  t: (key: string) => string;
}) {
  const INITIAL_LIMIT = 50;
  const [showAll, setShowAll] = useState(false);
  const visible = showAll ? relations : relations.slice(0, INITIAL_LIMIT);
  const hasMore = relations.length > INITIAL_LIMIT;

  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full min-w-[400px] text-xs">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="px-3 py-2 text-left font-medium">{t("kg.entity.columns.direction")}</th>
            <th className="px-3 py-2 text-left font-medium">{t("kg.entity.columns.relation")}</th>
            <th className="px-3 py-2 text-left font-medium">{t("kg.entity.columns.target")}</th>
            <th className="px-3 py-2 text-left font-medium">{t("kg.entity.columns.confidence")}</th>
          </tr>
        </thead>
        <tbody>
          {visible.map((rel) => (
            <tr key={rel.id} className="border-b last:border-0 hover:bg-muted/30">
              <td className="px-3 py-2">
                {rel.source_entity_id === entityId
                  ? t("kg.entity.direction.outgoing")
                  : t("kg.entity.direction.incoming")}
              </td>
              <td className="px-3 py-2 font-mono">{rel.relation_type}</td>
              <td className="px-3 py-2 font-mono text-muted-foreground">
                {rel.source_entity_id === entityId ? rel.target_entity_id.slice(0, 8) : rel.source_entity_id.slice(0, 8)}
              </td>
              <td className="px-3 py-2">{Math.round(rel.confidence * 100)}%</td>
            </tr>
          ))}
        </tbody>
      </table>
      {hasMore && !showAll && (
        <button
          onClick={() => setShowAll(true)}
          className="w-full py-1.5 text-xs text-muted-foreground hover:text-foreground border-t"
        >
          {t("kg.entity.showAll")} ({relations.length})
        </button>
      )}
    </div>
  );
});
