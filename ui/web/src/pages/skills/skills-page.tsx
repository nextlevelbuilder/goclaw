import { useState, useEffect } from "react";
import { Zap, Eye, RefreshCw, Upload, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useSkills, type SkillInfo } from "./hooks/use-skills";
import { SkillDetailDialog } from "./skill-detail-dialog";
import { SkillUploadDialog } from "./skill-upload-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";

export function SkillsPage() {
  const { skills, loading, refresh, getSkill, uploadSkill, deleteSkill } = useSkills();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && skills.length === 0);
  const [search, setSearch] = useState("");
  const [selectedSkill, setSelectedSkill] = useState<(SkillInfo & { content: string }) | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SkillInfo | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  const filtered = skills.filter(
    (s: SkillInfo) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      s.description.toLowerCase().includes(search.toLowerCase()),
  );

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, resetPage]);

  const handleViewSkill = async (name: string) => {
    setDetailLoading(true);
    try {
      const detail = await getSkill(name);
      if (detail) setSelectedSkill(detail);
    } finally {
      setDetailLoading(false);
    }
  };

  const handleUpload = async (file: File) => {
    await uploadSkill(file);
    refresh();
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

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Skills"
        description="Manage agent skills and capabilities"
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => setUploadOpen(true)} className="gap-1">
              <Upload className="h-3.5 w-3.5" /> Upload
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> Refresh
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder="Search skills..."
          className="max-w-sm"
        />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Zap}
            title={search ? "No matching skills" : "No skills"}
            description={search ? "Try a different search term." : "No skills have been registered yet."}
          />
        ) : (
          <div className="rounded-md border">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">Name</th>
                  <th className="px-4 py-3 text-left font-medium">Description</th>
                  <th className="px-4 py-3 text-left font-medium">Source</th>
                  <th className="px-4 py-3 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((skill: SkillInfo) => (
                  <tr key={skill.name} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <Zap className="h-4 w-4 text-muted-foreground" />
                        <span className="font-medium">{skill.name}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {skill.description || "No description"}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="outline">{skill.source || "file"}</Badge>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleViewSkill(skill.name)}
                          disabled={detailLoading}
                          className="gap-1"
                        >
                          <Eye className="h-3.5 w-3.5" /> View
                        </Button>
                        {skill.id && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setDeleteTarget(skill)}
                            className="gap-1 text-destructive hover:text-destructive"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
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
        />
      )}

      <SkillUploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        onUpload={handleUpload}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title="Delete Skill"
        description={`Are you sure you want to delete "${deleteTarget?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
    </div>
  );
}
