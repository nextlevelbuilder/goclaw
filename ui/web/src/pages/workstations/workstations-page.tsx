import { useState } from "react";
import { MonitorCog, Plus, RefreshCw, Trash2, ChevronDown, ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { formatDate } from "@/lib/format";
import { useWorkstations, type Workstation } from "./hooks/use-workstations";
import { WorkstationCreateDialog } from "./workstation-create-dialog";
import { WorkstationActivityTab } from "./workstation-activity-tab";

export function WorkstationsPage() {
  const { t } = useTranslation("workstations");
  const { workstations, loading, refresh, createWorkstation, deleteWorkstation } = useWorkstations();

  const spinning = useMinLoading(loading);
  const isEmpty = workstations.length === 0;
  const showSkeleton = useDeferredLoading(loading && isEmpty);

  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Workstation | null>(null);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  function toggleExpand(id: string) {
    setExpandedId((prev) => (prev === id ? null : id));
  }

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
              {t("common:refresh", "Refresh")}
            </Button>
            <Button size="sm" onClick={() => setCreateOpen(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" />
              {t("addWorkstation")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={4} />
        ) : isEmpty ? (
          <EmptyState
            icon={MonitorCog}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium w-8"></th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.key")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.backend")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.created")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {workstations.map((ws) => {
                  const isExpanded = expandedId === ws.id;
                  return (
                    <>
                      <tr
                        key={ws.id}
                        className="border-b last:border-0 hover:bg-muted/30 cursor-pointer"
                        onClick={() => toggleExpand(ws.id)}
                      >
                        <td className="px-4 py-3 text-muted-foreground">
                          {isExpanded ? (
                            <ChevronDown className="h-4 w-4" />
                          ) : (
                            <ChevronRight className="h-4 w-4" />
                          )}
                        </td>
                        <td className="px-4 py-3 font-medium">{ws.name}</td>
                        <td className="px-4 py-3 font-mono text-xs text-muted-foreground">{ws.workstation_key}</td>
                        <td className="px-4 py-3">
                          <Badge variant="outline">{t(`backend.${ws.backend_type}`)}</Badge>
                        </td>
                        <td className="px-4 py-3">
                          <Badge variant={ws.active ? "default" : "secondary"}>
                            {ws.active ? t("status.active") : t("status.inactive")}
                          </Badge>
                        </td>
                        <td className="px-4 py-3 text-muted-foreground">
                          {formatDate(new Date(ws.created_at))}
                        </td>
                        <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setDeleteTarget(ws)}
                            className="gap-1"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                            {t("actions.delete")}
                          </Button>
                        </td>
                      </tr>
                      {isExpanded && (
                        <tr key={`${ws.id}-detail`} className="bg-muted/10">
                          <td colSpan={7} className="px-4 py-4">
                            <Tabs defaultValue="activity">
                              <TabsList className="mb-3">
                                <TabsTrigger value="activity">{t("activity.title")}</TabsTrigger>
                              </TabsList>
                              <TabsContent value="activity">
                                <WorkstationActivityTab workstationId={ws.id} />
                              </TabsContent>
                            </Tabs>
                          </td>
                        </tr>
                      )}
                    </>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <WorkstationCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreate={async (params) => {
          await createWorkstation(params);
        }}
      />

      {deleteTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title={t("deleteDialog.title")}
          description={t("deleteDialog.description", { name: deleteTarget.name })}
          confirmLabel={t("deleteDialog.confirmLabel")}
          variant="destructive"
          onConfirm={async () => {
            await deleteWorkstation(deleteTarget.id);
            setDeleteTarget(null);
          }}
        />
      )}
    </div>
  );
}
