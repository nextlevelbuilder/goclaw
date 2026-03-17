import { useState, useMemo, useCallback, useEffect } from "react";
import { ClipboardList, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useTranslation } from "react-i18next";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { Pagination } from "@/components/shared/pagination";
import { usePagination } from "@/hooks/use-pagination";
import type { TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment } from "@/types/team";
import type { TeamMemberData } from "@/types/team";
import { taskStatusBadgeVariant, isTerminalStatus } from "./task-utils";
import { TaskDetailDialog } from "./task-detail-dialog";
import { buildTaskLookup, buildMemberLookup } from "../board/board-utils";

interface TaskListProps {
  tasks: TeamTaskData[];
  loading: boolean;
  teamId: string;
  members: TeamMemberData[];
  isTeamV2?: boolean;
  emojiLookup?: Map<string, string>;
  getTaskDetail: (teamId: string, taskId: string) => Promise<{
    task: TeamTaskData; comments: TeamTaskComment[];
    events: TeamTaskEvent[]; attachments: TeamTaskAttachment[];
  }>;
  deleteTask?: (teamId: string, taskId: string) => Promise<void>;
  deleteTasksBulk?: (teamId: string, taskIds: string[]) => Promise<number>;
}

export function TaskList({
  tasks, loading, teamId, members, isTeamV2, emojiLookup,
  getTaskDetail, deleteTask, deleteTasksBulk,
}: TaskListProps) {
  const { t } = useTranslation("teams");
  const [selectedTask, setSelectedTask] = useState<TeamTaskData | null>(null);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const taskLookup = useMemo(() => buildTaskLookup(tasks), [tasks]);
  const memberLookup = useMemo(() => buildMemberLookup(members), [members]);
  const { pageItems, pagination, setPage, setPageSize } = usePagination(tasks, { defaultPageSize: 20 });

  // "Select all" applies to terminal tasks on the current page only.
  const pageTerminalIds = useMemo(
    () => pageItems.filter((t) => isTerminalStatus(t.status)).map((t) => t.id),
    [pageItems],
  );

  // Clear selection when tasks change (e.g. after delete/refresh).
  useEffect(() => {
    setSelectedIds((prev) => {
      const taskIdSet = new Set(tasks.map((t) => t.id));
      const next = new Set([...prev].filter((id) => taskIdSet.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [tasks]);

  const toggleSelect = useCallback((taskId: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(taskId)) next.delete(taskId);
      else next.add(taskId);
      return next;
    });
  }, []);

  const toggleSelectAll = useCallback(() => {
    setSelectedIds((prev) => {
      const allPageSelected = pageTerminalIds.length > 0 && pageTerminalIds.every((id) => prev.has(id));
      if (allPageSelected) {
        // Deselect current page's terminal tasks, keep other pages' selections.
        const next = new Set(prev);
        for (const id of pageTerminalIds) next.delete(id);
        return next;
      }
      // Add current page's terminal tasks to selection.
      return new Set([...prev, ...pageTerminalIds]);
    });
  }, [pageTerminalIds]);

  const handleBulkDelete = useCallback(async () => {
    if (!deleteTasksBulk || selectedIds.size === 0) return;
    setDeleting(true);
    try {
      await deleteTasksBulk(teamId, [...selectedIds]);
      setSelectedIds(new Set());
      setConfirmOpen(false);
    } finally {
      setDeleting(false);
    }
  }, [deleteTasksBulk, teamId, selectedIds]);

  if (loading && tasks.length === 0) {
    return <div className="py-8 text-center text-sm text-muted-foreground">{t("tasks.loading")}</div>;
  }

  if (tasks.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-8 text-center">
        <ClipboardList className="h-8 w-8 text-muted-foreground/50" />
        <p className="text-sm text-muted-foreground">{t("tasks.noTasks")}</p>
        <p className="text-xs text-muted-foreground">{t("tasks.noTasksDescription")}</p>
      </div>
    );
  }

  const handleDelete = (e: React.MouseEvent, taskId: string) => {
    e.stopPropagation();
    if (!deleteTask) return;
    if (!window.confirm(t("tasks.deleteConfirm"))) return;
    deleteTask(teamId, taskId);
  };

  const hasBulkDelete = !!deleteTasksBulk && pageTerminalIds.length > 0;
  const allPageSelected = pageTerminalIds.length > 0 && pageTerminalIds.every((id) => selectedIds.has(id));
  const someSelected = selectedIds.size > 0 && !allPageSelected;

  const gridCols = hasBulkDelete
    ? "grid-cols-[36px_70px_1fr_90px_100px_60px_40px]"
    : "grid-cols-[70px_1fr_90px_100px_60px_40px]";

  return (
    <>
      {/* Bulk action bar */}
      {hasBulkDelete && selectedIds.size > 0 && (
        <div className="mb-2 flex items-center gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-2">
          <span className="text-sm font-medium">
            {t("tasks.selected", { count: selectedIds.size })}
          </span>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setConfirmOpen(true)}
          >
            <Trash2 className="mr-1.5 h-3.5 w-3.5" />
            {t("tasks.deleteSelected")}
          </Button>
        </div>
      )}

      <div className="overflow-x-auto rounded-lg border">
        <div className={`grid min-w-[500px] ${gridCols} items-center gap-2 border-b bg-muted/50 px-4 py-2.5 text-xs font-medium text-muted-foreground`}>
          {hasBulkDelete && (
            <label className="flex items-center justify-center">
              <input
                type="checkbox"
                className="h-4 w-4 cursor-pointer rounded border-muted-foreground/50 text-base accent-primary"
                checked={allPageSelected}
                ref={(el) => { if (el) el.indeterminate = someSelected; }}
                onChange={toggleSelectAll}
              />
            </label>
          )}
          <span>{t("tasks.columns.id")}</span>
          <span>{t("tasks.columns.subject")}</span>
          <span>{t("tasks.columns.status")}</span>
          <span>{t("tasks.columns.owner")}</span>
          <span>{t("tasks.columns.priority")}</span>
          <span />
        </div>
        {pageItems.map((task) => {
          const ownerEmoji = task.owner_agent_id && emojiLookup?.get(task.owner_agent_id);
          const isTerminal = isTerminalStatus(task.status);
          const isChecked = selectedIds.has(task.id);
          return (
            <div
              key={task.id}
              className={`grid min-w-[500px] cursor-pointer ${gridCols} items-center gap-2 border-b px-4 py-3 last:border-0 hover:bg-muted/30 ${isChecked ? "bg-destructive/5" : ""}`}
              onClick={() => setSelectedTask(task)}
            >
              {hasBulkDelete && (
                <label className="flex items-center justify-center" onClick={(e) => e.stopPropagation()}>
                  {isTerminal ? (
                    <input
                      type="checkbox"
                      className="h-4 w-4 cursor-pointer rounded border-muted-foreground/50 text-base accent-primary"
                      checked={isChecked}
                      onChange={() => toggleSelect(task.id)}
                    />
                  ) : (
                    <span className="h-4 w-4" />
                  )}
                </label>
              )}
              <span className="font-mono text-xs text-muted-foreground">{task.identifier || "\u2014"}</span>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{task.subject}</p>
                {task.description && (
                  <p className="truncate text-xs text-muted-foreground/70">{task.description}</p>
                )}
                {task.task_type && task.task_type !== "general" && (
                  <Badge variant="outline" className="mt-0.5 text-[10px]">{task.task_type}</Badge>
                )}
                {isTeamV2 && task.progress_percent != null && task.progress_percent > 0 && !isTerminal && (
                  <div className="mt-1 flex items-center gap-1.5">
                    <div className="h-1.5 flex-1 rounded-full bg-muted">
                      <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${task.progress_percent}%` }} />
                    </div>
                    <span className="text-[10px] text-muted-foreground">{task.progress_percent}%</span>
                  </div>
                )}
              </div>
              <div className="flex flex-wrap items-center gap-1">
                <Badge variant={taskStatusBadgeVariant(task.status)}>{task.status.replace(/_/g, " ")}</Badge>
                {isTeamV2 && task.followup_at && task.status === "in_progress" && (
                  <Badge variant="outline" className="border-amber-500/50 bg-amber-500/10 text-[10px] text-amber-700 dark:text-amber-400">
                    {t("tasks.badges.awaitingReply")}
                  </Badge>
                )}
              </div>
              <span className="flex items-center gap-1 truncate text-sm text-muted-foreground">
                {ownerEmoji && <span className="text-sm leading-none">{ownerEmoji}</span>}
                {task.owner_agent_key || "\u2014"}
              </span>
              <span className="text-sm text-muted-foreground">{task.priority}</span>
              <div>
                {deleteTask && isTerminal && (
                  <button
                    className="rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                    onClick={(e) => handleDelete(e, task.id)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      <Pagination
        page={pagination.page}
        pageSize={pagination.pageSize}
        total={pagination.total}
        totalPages={pagination.totalPages}
        onPageChange={setPage}
        onPageSizeChange={setPageSize}
      />

      {selectedTask && (
        <TaskDetailDialog
          task={selectedTask}
          teamId={teamId}
          isTeamV2={isTeamV2}
          onClose={() => setSelectedTask(null)}
          getTaskDetail={getTaskDetail}
          deleteTask={deleteTask}
          taskLookup={taskLookup}
          memberLookup={memberLookup}
          emojiLookup={emojiLookup}
          onNavigateTask={(taskId) => {
            const found = tasks.find((t) => t.id === taskId);
            if (found) setSelectedTask(found);
          }}
        />
      )}

      <ConfirmDeleteDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t("tasks.deleteBulkTitle")}
        description={t("tasks.deleteBulkConfirm", { count: selectedIds.size })}
        confirmValue="delete"
        confirmLabel={t("tasks.deleteSelected")}
        onConfirm={handleBulkDelete}
        loading={deleting}
      />
    </>
  );
}
