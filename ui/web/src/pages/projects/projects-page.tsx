import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { FolderKanban, Plus, RefreshCw, Pencil, Trash2, Plug } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useProjects, type ProjectData, type ProjectInput } from "./hooks/use-projects";
import { ProjectFormDialog } from "./project-form-dialog";
import { ProjectOverridesDialog } from "./project-overrides-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";

export function ProjectsPage() {
  const { t } = useTranslation("projects");
  const { t: tc } = useTranslation("common");
  const {
    projects, loading, refresh,
    createProject, updateProject, deleteProject,
    listOverrides, setOverride, removeOverride,
  } = useProjects();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && projects.length === 0);
  const [search, setSearch] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editProject, setEditProject] = useState<ProjectData | null>(null);
  const [overridesProject, setOverridesProject] = useState<ProjectData | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ProjectData | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  const filtered = projects.filter(
    (p) =>
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      p.slug.toLowerCase().includes(search.toLowerCase()),
  );

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, resetPage]);

  const handleCreate = async (data: ProjectInput) => {
    await createProject(data);
  };

  const handleEdit = async (data: ProjectInput) => {
    if (!editProject) return;
    await updateProject(editProject.id, data);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await deleteProject(deleteTarget.id);
      setDeleteTarget(null);
    } finally {
      setDeleteLoading(false);
    }
  };

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button size="sm" onClick={() => { setEditProject(null); setFormOpen(true); }} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("addProject")}
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
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={FolderKanban}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.channel")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.createdBy")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((proj) => (
                  <tr key={proj.id} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <div>
                        <span className="font-medium">{proj.name}</span>
                        <span className="ml-1.5 font-mono text-xs text-muted-foreground">({proj.slug})</span>
                      </div>
                      {proj.description && (
                        <p className="text-xs text-muted-foreground mt-0.5 truncate max-w-xs">{proj.description}</p>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      {proj.channel_type ? (
                        <div>
                          <Badge variant="outline">{proj.channel_type}</Badge>
                          {proj.chat_id && (
                            <span className="ml-1.5 font-mono text-[11px] text-muted-foreground">{proj.chat_id}</span>
                          )}
                        </div>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={proj.status === "active" ? "default" : "secondary"}>
                        {t(`status.${proj.status}` as const)}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">{proj.created_by || "—"}</td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setOverridesProject(proj)}
                          title={t("manageOverrides")}
                        >
                          <Plug className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => { setEditProject(proj); setFormOpen(true); }}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDeleteTarget(proj)}
                          className="text-destructive hover:text-destructive"
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

      <ProjectFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        project={editProject}
        onSubmit={editProject ? handleEdit : handleCreate}
      />

      {overridesProject && (
        <ProjectOverridesDialog
          open={!!overridesProject}
          onOpenChange={(open) => !open && setOverridesProject(null)}
          project={overridesProject}
          onLoadOverrides={listOverrides}
          onSaveOverride={setOverride}
          onRemoveOverride={removeOverride}
        />
      )}

      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { name: deleteTarget?.name })}
        confirmValue={deleteTarget?.name || ""}
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
    </div>
  );
}
