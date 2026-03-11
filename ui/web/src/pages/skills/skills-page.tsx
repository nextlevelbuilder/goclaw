import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Zap, Pencil, RefreshCw, Upload, Trash2, ScanSearch } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useSkills, type SkillInfo } from "./hooks/use-skills";
import { SkillDetailDialog } from "./skill-detail-dialog";
import { SkillUploadDialog } from "./skill-upload-dialog";
import { SkillEditDialog } from "./skill-edit-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";

const visibilityColor: Record<string, string> = {
  public: "default",
  internal: "secondary",
  private: "outline",
};

export function SkillsPage() {
  const { t } = useTranslation("skills");
  const {
    skills, loading, refresh, getSkill, uploadSkill, updateSkill, deleteSkill,
    getSkillVersions, getSkillFiles, getSkillFileContent, rescanDeps,
  } = useSkills();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && skills.length === 0);
  const [search, setSearch] = useState("");
  const [selectedSkill, setSelectedSkill] = useState<(SkillInfo & { content: string }) | null>(null);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<SkillInfo | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SkillInfo | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [rescanning, setRescanning] = useState(false);

  const filtered = skills.filter(
    (s: SkillInfo) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      s.description.toLowerCase().includes(search.toLowerCase()),
  );

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, resetPage]);

  const handleViewSkill = async (name: string) => {
    const detail = await getSkill(name);
    if (detail) setSelectedSkill(detail);
  };

  const handleUpload = async (file: File) => {
    await uploadSkill(file);
    refresh();
  };

  const handleCycleVisibility = async (skill: SkillInfo) => {
    if (!skill.id) return;
    const order = ["private", "internal", "public"] as const;
    const idx = order.indexOf(skill.visibility as typeof order[number]);
    const next = order[(idx + 1) % order.length];
    await updateSkill(skill.id, { visibility: next });
  };

  const handleDelete = async () => {
    if (!deleteTarget?.id) return;
    setDeleteLoading(true);
    try {
      await deleteSkill(deleteTarget.id);
      setDeleteTarget(null);
      refresh();
    } finally {
      setDeleteLoading(false);
    }
  };

  const handleRescanDeps = async () => {
    setRescanning(true);
    try {
      await rescanDeps();
    } finally {
      setRescanning(false);
    }
  };

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => setUploadOpen(true)} className="gap-1">
              <Upload className="h-3.5 w-3.5" /> {t("upload.button")}
            </Button>
            <Button variant="outline" size="sm" onClick={handleRescanDeps} disabled={rescanning} className="gap-1">
              <ScanSearch className="h-3.5 w-3.5" /> {t("deps.rescan")}
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {t("refresh", { ns: "common" })}
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
            icon={Zap}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.description")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.source")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.visibility")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((skill: SkillInfo) => (
                  <tr key={skill.name} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <Zap className="h-4 w-4 text-muted-foreground" />
                        <button
                          type="button"
                          className="font-medium text-left hover:underline cursor-pointer"
                          onClick={() => handleViewSkill(skill.slug ?? skill.name)}
                        >
                          {skill.name}
                        </button>
                        {skill.is_system && (
                          <Badge variant="outline" className="border-blue-500 text-blue-600 text-[10px]">
                            {t("system")}
                          </Badge>
                        )}
                        {skill.version ? (
                          <span className="text-xs text-muted-foreground">v{skill.version}</span>
                        ) : null}
                      </div>
                    </td>
                    <td className="max-w-xs truncate px-4 py-3 text-muted-foreground">
                      {skill.description || t("noDescription")}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="outline">{skill.source || "file"}</Badge>
                    </td>
                    <td className="px-4 py-3">
                      {skill.visibility && (
                        skill.id ? (
                          <button
                            type="button"
                            onClick={() => handleCycleVisibility(skill)}
                            title={t("visibility.clickToCycle")}
                          >
                            <Badge
                              variant={visibilityColor[skill.visibility] as "default" | "secondary" | "outline"}
                              className="cursor-pointer hover:opacity-80 transition-opacity"
                            >
                              {skill.visibility}
                            </Badge>
                          </button>
                        ) : (
                          <Badge variant={visibilityColor[skill.visibility] as "default" | "secondary" | "outline"}>
                            {skill.visibility}
                          </Badge>
                        )
                      )}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        {skill.id && (
                          <>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setEditTarget(skill)}
                              className="gap-1"
                            >
                              <Pencil className="h-3.5 w-3.5" />
                            </Button>
                            {!skill.is_system && (
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => setDeleteTarget(skill)}
                                className="gap-1 text-destructive hover:text-destructive"
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                              </Button>
                            )}
                          </>
                        )}
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

      {selectedSkill && (
        <SkillDetailDialog
          skill={selectedSkill}
          onClose={() => setSelectedSkill(null)}
          getSkillVersions={getSkillVersions}
          getSkillFiles={getSkillFiles}
          getSkillFileContent={getSkillFileContent}
        />
      )}

      {editTarget && (
        <SkillEditDialog
          skill={editTarget}
          onClose={() => setEditTarget(null)}
          onSave={async (id, updates) => {
            await updateSkill(id, updates);
            setEditTarget(null);
          }}
        />
      )}

      <SkillUploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        onUpload={handleUpload}
      />

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
