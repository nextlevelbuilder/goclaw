import { useState, useEffect, useCallback } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { GitFork } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useKGTraversal } from "./hooks/use-knowledge-graph";
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
  const [relations, setRelations] = useState<KGRelation[]>([]);
  const [loadingRels, setLoadingRels] = useState(false);
  const { results: traversalResults, traversing, traverse } = useKGTraversal(agentId);

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

  const handleTraverse = () => {
    if (!entity) return;
    traverse(entity.id, entity.user_id, 2);
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !traversing && onOpenChange(v)}>
      <DialogContent aria-describedby={undefined} className="max-w-3xl max-h-[85vh] flex flex-col">
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

          {/* Relations */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <h4 className="text-sm font-medium">{t("kg.entity.relations")}</h4>
              <Button variant="outline" size="sm" onClick={handleTraverse} disabled={traversing} className="gap-1">
                <GitFork className="h-3.5 w-3.5" />
                {traversing ? t("kg.entity.traversing") : t("kg.entity.traverse")}
              </Button>
            </div>
            {loadingRels ? (
              <p className="text-xs text-muted-foreground">{t("kg.entity.loading")}</p>
            ) : relations.length === 0 ? (
              <p className="text-xs text-muted-foreground">{t("kg.entity.noRelations")}</p>
            ) : (
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
                    {relations.map((rel) => (
                      <tr key={rel.id} className="border-b last:border-0 hover:bg-muted/30">
                        <td className="px-3 py-2">
                          {rel.source_entity_id === entity?.id
                            ? t("kg.entity.direction.outgoing")
                            : t("kg.entity.direction.incoming")}
                        </td>
                        <td className="px-3 py-2 font-mono">{rel.relation_type}</td>
                        <td className="px-3 py-2 font-mono text-muted-foreground">
                          {rel.source_entity_id === entity?.id ? rel.target_entity_id.slice(0, 8) : rel.source_entity_id.slice(0, 8)}
                        </td>
                        <td className="px-3 py-2">{Math.round(rel.confidence * 100)}%</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {/* Traversal results */}
          {traversalResults.length > 0 && (
            <div>
              <h4 className="text-sm font-medium mb-2">
                {t("kg.entity.traversalResults", { count: traversalResults.length })}
              </h4>
              <div className="space-y-1">
                {traversalResults.map((tr, i) => (
                  <div key={i} className="flex items-center gap-2 text-xs rounded border p-2">
                    <Badge variant="outline" className="text-[10px]">depth {tr.depth}</Badge>
                    {tr.via && <span className="font-mono text-muted-foreground">—[{tr.via}]→</span>}
                    <span className="font-medium">{tr.entity.name}</span>
                    <Badge variant="secondary" className="text-[10px]">{tr.entity.entity_type}</Badge>
                    {tr.entity.description && (
                      <span className="text-muted-foreground truncate max-w-[200px]">{tr.entity.description}</span>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
