import { useState } from "react";
import { Clock, Plus, Play, Trash2, History, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useCron, type CronJob, type CronRunLogEntry } from "./hooks/use-cron";
import { CronFormDialog } from "./cron-form-dialog";
import { CronRunLogDialog } from "./cron-run-log-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";

function formatSchedule(job: CronJob): string {
  const s = job.schedule;
  if (s.kind === "every" && s.everyMs) {
    const sec = s.everyMs / 1000;
    if (sec < 60) return `every ${sec}s`;
    if (sec < 3600) return `every ${Math.round(sec / 60)}m`;
    return `every ${Math.round(sec / 3600)}h`;
  }
  if (s.kind === "cron" && s.expr) return s.expr;
  if (s.kind === "at" && s.atMs) return `once at ${new Date(s.atMs).toLocaleString()}`;
  return s.kind;
}

export function CronPage() {
  const { jobs, loading, refresh, createJob, toggleJob, deleteJob, runJob, getRunLog } = useCron();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && jobs.length === 0);
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<CronJob | null>(null);
  const [runLogTarget, setRunLogTarget] = useState<CronJob | null>(null);
  const [runLogEntries, setRunLogEntries] = useState<CronRunLogEntry[]>([]);
  const [runLogLoading, setRunLogLoading] = useState(false);
  const [toggleTarget, setToggleTarget] = useState<{ job: CronJob; enabled: boolean } | null>(null);

  const { pageItems, pagination, setPage, setPageSize } = usePagination(jobs);

  const handleShowRunLog = async (job: CronJob) => {
    setRunLogTarget(job);
    setRunLogLoading(true);
    try {
      const entries = await getRunLog(job.id);
      setRunLogEntries(entries);
    } finally {
      setRunLogLoading(false);
    }
  };

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Cron"
        description="Schedule recurring agent tasks"
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> Refresh
            </Button>
            <Button size="sm" onClick={() => setShowCreate(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> New Job
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : jobs.length === 0 ? (
          <EmptyState
            icon={Clock}
            title="No cron jobs"
            description="Create a cron job to schedule recurring agent tasks."
            action={
              <Button size="sm" onClick={() => setShowCreate(true)} className="gap-1">
                <Plus className="h-3.5 w-3.5" /> New Job
              </Button>
            }
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[700px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">Enabled</th>
                  <th className="px-4 py-3 text-left font-medium">Name</th>
                  <th className="px-4 py-3 text-left font-medium">Schedule</th>
                  <th className="px-4 py-3 text-left font-medium">Message</th>
                  <th className="px-4 py-3 text-left font-medium">Agent</th>
                  <th className="px-4 py-3 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((job: CronJob) => (
                  <tr key={job.id} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <Switch
                        checked={job.enabled}
                        onCheckedChange={(checked: boolean) => setToggleTarget({ job, enabled: checked })}
                      />
                    </td>
                    <td className="px-4 py-3 font-medium">{job.name}</td>
                    <td className="px-4 py-3">
                      <Badge variant="outline">{formatSchedule(job)}</Badge>
                    </td>
                    <td className="max-w-[200px] truncate px-4 py-3 text-muted-foreground">
                      {job.payload?.message}
                    </td>
                    <td className="px-4 py-3">
                      {job.agentId ? (
                        <Badge variant="secondary">{job.agentId}</Badge>
                      ) : (
                        <span className="text-muted-foreground">default</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          title="Run now"
                          onClick={() => runJob(job.id)}
                        >
                          <Play className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          title="Run history"
                          onClick={() => handleShowRunLog(job)}
                        >
                          <History className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          title="Delete"
                          onClick={() => setDeleteTarget(job)}
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

      <CronFormDialog
        open={showCreate}
        onOpenChange={setShowCreate}
        onSubmit={createJob}
      />

      {toggleTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setToggleTarget(null)}
          title={toggleTarget.enabled ? "Enable Cron Job" : "Disable Cron Job"}
          description={
            toggleTarget.enabled
              ? `Enable "${toggleTarget.job.name}"? It will start running on schedule.`
              : `Disable "${toggleTarget.job.name}"? It will stop running until re-enabled.`
          }
          confirmLabel={toggleTarget.enabled ? "Enable" : "Disable"}
          variant={toggleTarget.enabled ? "default" : "destructive"}
          onConfirm={async () => {
            await toggleJob(toggleTarget.job.id, toggleTarget.enabled);
            setToggleTarget(null);
          }}
        />
      )}

      {deleteTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title="Delete Cron Job"
          description={`Delete "${deleteTarget.name}"? This cannot be undone.`}
          confirmLabel="Delete"
          variant="destructive"
          onConfirm={async () => {
            await deleteJob(deleteTarget.id);
            setDeleteTarget(null);
          }}
        />
      )}

      {runLogTarget && (
        <CronRunLogDialog
          open
          onOpenChange={() => setRunLogTarget(null)}
          jobName={runLogTarget.name}
          entries={runLogEntries}
          loading={runLogLoading}
        />
      )}
    </div>
  );
}
