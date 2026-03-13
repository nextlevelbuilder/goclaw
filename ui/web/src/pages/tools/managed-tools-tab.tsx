import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { Package, Pencil, RefreshCw, Upload, Trash2, ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { cn } from "@/lib/utils";
import { ROUTES } from "@/lib/constants";
import { useManagedTools, type ManagedToolInfo } from "./hooks/use-managed-tools";
import { ManagedToolUploadDialog } from "./managed-tool-upload-dialog";
import { ManagedToolEditDialog } from "./managed-tool-edit-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";

const visibilityColor: Record<string, string> = {
  public: "default",
  internal: "secondary",
  private: "outline",
};

export function ManagedToolsTab() {
  const { t } = useTranslation("tools");
  const navigate = useNavigate();
  const {
    managedTools, loading, refresh, uploadTool, updateTool, deleteTool, toggleTool,
  } = useManagedTools();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && managedTools.length === 0);
  const [search, setSearch] = useState("");
  const [uploadOpen, setUploadOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<ManagedToolInfo | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ManagedToolInfo | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [toggling, setToggling] = useState<string | null>(null);

  const filtered = managedTools.filter(
    (t: ManagedToolInfo) =>
      t.name.toLowerCase().includes(search.toLowerCase()) ||
      t.description.toLowerCase().includes(search.toLowerCase()),
  );

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, resetPage]);

  const handleUpload = async (file: File) => {
    await uploadTool(file);
    refresh();
  };

  const handleDelete = async () => {
    if (!deleteTarget?.id) return;
    setDeleteLoading(true);
    try {
      await deleteTool(deleteTarget.id);
      setDeleteTarget(null);
      refresh();
    } finally {
      setDeleteLoading(false);
    }
  };

  const handleToggle = async (tool: ManagedToolInfo, enabled: boolean) => {
    if (!tool.id) return;
    setToggling(tool.id);
    try {
      await toggleTool(tool.id, enabled);
    } finally {
      setToggling(null);
    }
  };

  const handleCycleVisibility = async (tool: ManagedToolInfo) => {
    if (!tool.id) return;
    const order = ["private", "internal", "public"] as const;
    const idx = order.indexOf(tool.visibility as typeof order[number]);
    const next = order[(idx + 1) % order.length];
    await updateTool(tool.id, { visibility: next });
  };

  const handleOpen = (tool: ManagedToolInfo) => {
    navigate(ROUTES.TOOL_DETAIL.replace(":id", tool.id));
  };

  return (
    <div className="mt-4">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("managed.searchPlaceholder")}
          className="max-w-sm"
        />
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => setUploadOpen(true)} className="gap-1">
            <Upload className="h-3.5 w-3.5" /> {t("managed.uploadButton")}
          </Button>
          <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
          </Button>
        </div>
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Package}
            title={search ? t("managed.noMatchTitle") : t("managed.emptyTitle")}
            description={search ? t("managed.noMatchDescription") : t("managed.emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("managed.columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("managed.columns.description")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("managed.columns.runtime")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("managed.columns.version")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("managed.columns.status")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("managed.columns.visibility")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("managed.columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((tool: ManagedToolInfo) => {
                  const isArchived = tool.status === "archived";
                  const isDisabled = tool.enabled === false;
                  return (
                    <tr key={tool.id} className={cn("border-b last:border-0 hover:bg-muted/30", (isArchived || isDisabled) && "opacity-60")}>
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2 flex-wrap">
                          <Package className="h-4 w-4 text-muted-foreground shrink-0" />
                          <button
                            type="button"
                            className="font-medium text-left hover:underline cursor-pointer"
                            onClick={() => handleOpen(tool)}
                          >
                            {tool.name}
                          </button>
                          {tool.is_system && (
                            <Badge variant="outline" className="border-blue-500 text-blue-600 text-[10px]">
                              {t("managed.system")}
                            </Badge>
                          )}
                        </div>
                      </td>
                      <td className="max-w-xs truncate px-4 py-3 text-muted-foreground">
                        {tool.description || t("managed.noDescription")}
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {tool.runtime || "—"}
                      </td>
                      <td className="px-4 py-3">
                        <span className="text-xs text-muted-foreground">v{tool.version}</span>
                      </td>
                      <td className="px-4 py-3">
                        <Badge
                          variant="outline"
                          className={cn(
                            "text-[10px] w-fit",
                            isArchived
                              ? "border-amber-500 text-amber-600 dark:border-amber-600 dark:text-amber-400"
                              : "border-emerald-500 text-emerald-600 dark:border-emerald-600 dark:text-emerald-400",
                          )}
                        >
                          {isArchived ? t("managed.status.archived") : t("managed.status.active")}
                        </Badge>
                      </td>
                      <td className="px-4 py-3">
                        {tool.visibility && (
                          <button
                            type="button"
                            onClick={() => handleCycleVisibility(tool)}
                            title={t("managed.visibility.clickToCycle")}
                          >
                            <Badge
                              variant={visibilityColor[tool.visibility] as "default" | "secondary" | "outline"}
                              className="cursor-pointer hover:opacity-80 transition-opacity"
                            >
                              {tool.visibility}
                            </Badge>
                          </button>
                        )}
                      </td>
                      <td className="px-4 py-3 text-right">
                        <div className="flex items-center justify-end gap-2">
                          <Switch
                            size="sm"
                            checked={tool.enabled !== false}
                            disabled={toggling === tool.id}
                            onCheckedChange={(checked) => handleToggle(tool, checked)}
                            title={tool.enabled !== false ? t("managed.toggle.disable") : t("managed.toggle.enable")}
                          />
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleOpen(tool)}
                            className="gap-1"
                            title="Open"
                          >
                            <ExternalLink className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setEditTarget(tool)}
                            className="gap-1"
                          >
                            <Pencil className="h-3.5 w-3.5" />
                          </Button>
                          {!tool.is_system && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setDeleteTarget(tool)}
                              className="gap-1 text-destructive hover:text-destructive"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <Pagination
              page={pagination.page}
              pageSize={pagination.pageSize}
              total={pagination.total}
              totalPages={pagination.totalPages}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
          </div>
        )}
      </div>

      {editTarget && (
        <ManagedToolEditDialog
          tool={editTarget}
          onClose={() => setEditTarget(null)}
          onSave={async (id, updates) => {
            await updateTool(id, updates);
            setEditTarget(null);
          }}
        />
      )}

      <ManagedToolUploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        onUpload={handleUpload}
      />

      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("managed.delete.title")}
        description={t("managed.delete.description", { name: deleteTarget?.name })}
        confirmValue={deleteTarget?.name || ""}
        confirmLabel={t("managed.delete.confirmLabel")}
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
    </div>
  );
}
