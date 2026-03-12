import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Wrench, Plus, RefreshCw, Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useCustomTools, type CustomToolData, type CustomToolInput } from "./hooks/use-custom-tools";
import { CustomToolFormDialog } from "./custom-tool-form-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useRef } from "react";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";

export function CustomToolsPage() {
  const { t } = useTranslation("tools");
  const { t: tc } = useTranslation("common");
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [formOpen, setFormOpen] = useState(false);
  const [editTool, setEditTool] = useState<CustomToolData | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<CustomToolData | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  const pendingSearchRef = useRef("");
  const flushSearch = useDebouncedCallback(() => {
    setDebouncedSearch(pendingSearchRef.current);
    setPage(1);
  }, 300);

  const handleSearchChange = (v: string) => {
    setSearch(v);
    pendingSearchRef.current = v;
    flushSearch();
  };

  const { tools, total, loading, refresh, createTool, updateTool, deleteTool } = useCustomTools({
    search: debouncedSearch || undefined,
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && tools.length === 0);
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  const handleCreate = async (data: CustomToolInput) => {
    await createTool(data);
  };

  const handleEdit = async (data: CustomToolInput) => {
    if (!editTool) return;
    await updateTool(editTool.id, data);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await deleteTool(deleteTarget.id);
      setDeleteTarget(null);
    } finally {
      setDeleteLoading(false);
    }
  };

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("custom.title")}
        description={t("custom.description")}
        actions={
          <div className="flex gap-2">
            <Button size="sm" onClick={() => { setEditTool(null); setFormOpen(true); }} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("custom.createTool")}
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={handleSearchChange}
          placeholder={t("custom.searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : tools.length === 0 ? (
          <EmptyState
            icon={Wrench}
            title={debouncedSearch ? t("custom.noMatchTitle") : t("custom.emptyTitle")}
            description={debouncedSearch ? t("custom.noMatchDescription") : t("custom.emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("custom.columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("custom.columns.description")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("custom.columns.scope")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("custom.columns.enabled")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("custom.columns.timeout")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("custom.columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {tools.map((tool) => (
                  <tr key={tool.id} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <Wrench className="h-4 w-4 text-muted-foreground" />
                        <span className="font-medium">{tool.name}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {tool.description || t("custom.noDescription")}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={tool.agent_id ? "secondary" : "outline"}>
                        {tool.agent_id ? t("custom.scope.agent") : t("custom.scope.global")}
                      </Badge>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={tool.enabled ? "default" : "secondary"}>
                        {tool.enabled ? tc("yes") : tc("no")}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">{tool.timeout_seconds}s</td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => { setEditTool(tool); setFormOpen(true); }}
                          className="gap-1"
                        >
                          <Pencil className="h-3.5 w-3.5" /> {tc("edit")}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDeleteTarget(tool)}
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

      <CustomToolFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        tool={editTool}
        onSubmit={editTool ? handleEdit : handleCreate}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("custom.delete.title")}
        description={t("custom.delete.description", { name: deleteTarget?.name })}
        confirmLabel={t("custom.delete.confirmLabel")}
        variant="destructive"
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
    </div>
  );
}
