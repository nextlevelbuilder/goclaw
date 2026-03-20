import { useState, useMemo } from "react";
import { useParams, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { Clock, Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useCron, type CronJob } from "./hooks/use-cron";
import { CronFormDialog } from "./cron-form-dialog";
import { CronDetailPage } from "./cron-detail-page";
import { CronListRow } from "./cron-list-row";
import { Pagination } from "@/components/shared/pagination";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useMinLoading } from "@/hooks/use-min-loading";
import { usePagination } from "@/hooks/use-pagination";

export function CronPage() {
  const { id: detailId } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { jobs, loading, refreshing, refresh, createJob, toggleJob, deleteJob, runJob, getRunLog, updateJob } = useCron();
  const spinning = useMinLoading(refreshing);
  const showSkeleton = useDeferredLoading(loading && jobs.length === 0);

  const { t } = useTranslation("cron");
  const { t: tc } = useTranslation("common");

  const [search, setSearch] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<CronJob | null>(null);

  // Detail routing
  const detailJob = detailId ? jobs.find((j) => j.id === detailId) : null;
  if (detailJob) {
    return (
      <CronDetailPage
        job={detailJob}
        onBack={() => navigate("/cron")}
        onRun={runJob}
        onToggle={toggleJob}
        onDelete={async (id) => {
          await deleteJob(id);
          navigate("/cron");
        }}
        onUpdate={updateJob}
        getRunLog={getRunLog}
        onRefresh={refresh}
      />
    );
  }

  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    if (!q) return jobs;
    return jobs.filter(
      (j) =>
        j.name.toLowerCase().includes(q) ||
        (j.payload?.message ?? "").toLowerCase().includes(q),
    );
  }, [jobs, search]);

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  // Reset page when search changes
  const [prevSearch, setPrevSearch] = useState(search);
  if (search !== prevSearch) {
    setPrevSearch(search);
    resetPage();
  }

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
            </Button>
            <Button size="sm" onClick={() => setShowCreate(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("newJob")}
            </Button>
          </div>
        }
      />

      {/* Toolbar */}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-6">
        {showSkeleton ? (
          <TableSkeleton />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Clock}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? t("noMatchDescription") : t("emptyDescription")}
            action={
              !search ? (
                <Button size="sm" onClick={() => setShowCreate(true)} className="gap-1">
                  <Plus className="h-3.5 w-3.5" /> {t("newJob")}
                </Button>
              ) : undefined
            }
          />
        ) : (
          <>
            <div className="mt-4 flex flex-col gap-2">
              {pageItems.map((job) => (
                <CronListRow
                  key={job.id}
                  job={job}
                  onClick={() => navigate(`/cron/${job.id}`)}
                  onRun={() => runJob(job.id)}
                  onDelete={() => setDeleteTarget(job)}
                />
              ))}
            </div>
            <div className="mt-4">
              <Pagination
                page={pagination.page}
                pageSize={pagination.pageSize}
                total={pagination.total}
                totalPages={pagination.totalPages}
                onPageChange={setPage}
                onPageSizeChange={setPageSize}
              />
            </div>
          </>
        )}
      </div>

      <CronFormDialog
        open={showCreate}
        onOpenChange={setShowCreate}
        onSubmit={createJob}
      />

      {deleteTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title={t("delete.title")}
          description={t("delete.description", { name: deleteTarget.name })}
          confirmLabel={t("delete.confirmLabel")}
          variant="destructive"
          onConfirm={async () => {
            await deleteJob(deleteTarget.id);
            setDeleteTarget(null);
          }}
        />
      )}

    </div>
  );
}
