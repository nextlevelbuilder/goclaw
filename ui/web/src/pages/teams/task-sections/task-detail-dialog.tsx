import { useState, useEffect, useCallback } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { formatDate } from "@/lib/format";
import { toast } from "@/stores/use-toast-store";
import type { TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment } from "@/types/team";
import { taskStatusBadgeVariant, isTerminalStatus } from "./task-utils";

interface TaskDetailDialogProps {
  task: TeamTaskData;
  teamId: string;
  isTeamV2?: boolean;
  onClose: () => void;
  getTaskDetail: (teamId: string, taskId: string) => Promise<{
    task: TeamTaskData; comments: TeamTaskComment[];
    events: TeamTaskEvent[]; attachments: TeamTaskAttachment[];
  }>;
  deleteTask?: (teamId: string, taskId: string) => Promise<void>;
  taskLookup?: Map<string, string>;
  memberLookup?: Map<string, string>;
  emojiLookup?: Map<string, string>;
  onNavigateTask?: (taskId: string) => void;
}

export function TaskDetailDialog({
  task, teamId, isTeamV2, onClose,
  getTaskDetail, deleteTask, taskLookup, memberLookup, emojiLookup, onNavigateTask,
}: TaskDetailDialogProps) {
  const { t } = useTranslation("teams");
  const [events, setEvents] = useState<TeamTaskEvent[]>([]);
  const [attachments, setAttachments] = useState<TeamTaskAttachment[]>([]);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const loadDetail = useCallback(async () => {
    try {
      const res = await getTaskDetail(teamId, task.id);
      setEvents(res.events ?? []);
      setAttachments(res.attachments ?? []);
    } catch { /* partial data acceptable */ }
  }, [getTaskDetail, teamId, task.id]);

  useEffect(() => { loadDetail(); }, [loadDetail]);

  const resolveMember = (id?: string) =>
    (id && memberLookup?.get(id)) || undefined;

  const resolveEmoji = (id?: string) =>
    (id && emojiLookup?.get(id)) || undefined;

  const handleDelete = async () => {
    if (!deleteTask) return;
    setDeleting(true);
    try {
      await deleteTask(teamId, task.id);
      toast.success(t("toast.taskDeleted"));
      onClose();
    } catch {
      toast.error(t("toast.failedDeleteTask"));
    } finally {
      setDeleting(false);
      setConfirmDelete(false);
    }
  };

  const ownerEmoji = resolveEmoji(task.owner_agent_id);
  const canDelete = deleteTask && isTerminalStatus(task.status);

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] w-[95vw] flex flex-col sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {task.identifier && (
              <Badge variant="outline" className="font-mono text-xs">{task.identifier}</Badge>
            )}
            {t("tasks.detail.title")}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4 overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6">
          {/* Subject */}
          <div className="rounded-md border p-3">
            <p className="mb-1 text-xs font-medium text-muted-foreground">{t("tasks.detail.subject")}</p>
            <p className="text-sm font-medium">{task.subject}</p>
          </div>

          {/* Follow-up banner (V2) */}
          {isTeamV2 && task.followup_at && task.status === "in_progress" && (
            <div className="rounded-md border border-amber-500/30 bg-amber-500/5 p-3">
              <p className="mb-1 text-xs font-semibold text-amber-700 dark:text-amber-400">
                {t("tasks.detail.followupStatus")}
              </p>
              {task.followup_message && (
                <p className="text-sm">
                  <span className="text-xs text-muted-foreground">{t("tasks.detail.followupMessage")}</span>{" "}
                  {task.followup_message}
                </p>
              )}
              <div className="mt-1 flex flex-wrap gap-3 text-xs text-muted-foreground">
                <span>
                  {task.followup_max && task.followup_max > 0
                    ? t("tasks.detail.followupCountMax", { count: task.followup_count ?? 0, max: task.followup_max })
                    : t("tasks.detail.followupCount", { count: task.followup_count ?? 0 })}
                </span>
                {task.followup_at && (
                  <span>
                    {task.followup_max && task.followup_max > 0 && (task.followup_count ?? 0) >= task.followup_max
                      ? t("tasks.detail.followupDone")
                      : `${t("tasks.detail.followupNext")} ${formatDate(task.followup_at)}`}
                  </span>
                )}
              </div>
            </div>
          )}

          {/* Progress bar (V2) */}
          {isTeamV2 && task.progress_percent != null && task.progress_percent > 0 && !isTerminalStatus(task.status) && (
            <div className="space-y-1">
              <div className="flex justify-between text-xs text-muted-foreground">
                <span>{t("tasks.detail.progress")}</span>
                <span>{task.progress_percent}%</span>
              </div>
              <div className="h-2 w-full rounded-full bg-muted">
                <div className="h-2 rounded-full bg-primary transition-all" style={{ width: `${task.progress_percent}%` }} />
              </div>
              {task.progress_step && <p className="text-xs text-muted-foreground">{task.progress_step}</p>}
            </div>
          )}

          {/* Summary grid */}
          <div className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
            <div>
              <span className="text-muted-foreground">{t("tasks.detail.status")}</span>{" "}
              <Badge variant={taskStatusBadgeVariant(task.status)} className="text-xs">{task.status.replace(/_/g, " ")}</Badge>
            </div>
            <div>
              <span className="text-muted-foreground">{t("tasks.detail.priority")}</span>{" "}
              <span className="font-medium">{task.priority}</span>
            </div>
            <div>
              <span className="text-muted-foreground">{t("tasks.detail.owner")}</span>{" "}
              <span className="font-medium">
                {ownerEmoji && <span className="mr-1">{ownerEmoji}</span>}
                {resolveMember(task.owner_agent_id) || task.owner_agent_key || "\u2014"}
              </span>
            </div>
            {task.task_type && task.task_type !== "general" && (
              <div>
                <span className="text-muted-foreground">{t("tasks.detail.type")}</span>{" "}
                <Badge variant="outline" className="text-xs">{task.task_type}</Badge>
              </div>
            )}
            {task.created_at && (
              <div><span className="text-muted-foreground">{t("tasks.detail.created")}</span> {formatDate(task.created_at)}</div>
            )}
            {task.updated_at && (
              <div><span className="text-muted-foreground">{t("tasks.detail.updated")}</span> {formatDate(task.updated_at)}</div>
            )}
          </div>

          {/* Blocked by */}
          {task.blocked_by && task.blocked_by.length > 0 && (
            <div className="text-sm">
              <span className="text-muted-foreground">{t("tasks.detail.blockedBy")}</span>{" "}
              <div className="mt-1 flex flex-wrap gap-1">
                {task.blocked_by.map((id) => (
                  <Badge
                    key={id}
                    variant="outline"
                    className={"text-xs" + (onNavigateTask ? " cursor-pointer hover:bg-accent" : "")}
                    onClick={onNavigateTask ? () => onNavigateTask(id) : undefined}
                  >
                    {taskLookup?.get(id) || id.slice(0, 8)}
                  </Badge>
                ))}
              </div>
            </div>
          )}

          {/* Description */}
          {task.description && (
            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs font-medium text-muted-foreground">{t("tasks.detail.description")}</p>
              <pre className="whitespace-pre-wrap break-words text-sm">{task.description}</pre>
            </div>
          )}

          {/* Result */}
          {task.result && (
            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs font-medium text-muted-foreground">{t("tasks.detail.result")}</p>
              <pre className="max-h-[40vh] overflow-y-auto whitespace-pre-wrap break-words text-sm">{task.result}</pre>
            </div>
          )}

          {/* Attachments (V2) */}
          {isTeamV2 && attachments.length > 0 && (
            <div className="rounded-md border p-3">
              <p className="mb-2 text-xs font-medium text-muted-foreground">{t("tasks.detail.attachments")}</p>
              <div className="space-y-1">
                {attachments.map((a) => (
                  <div key={a.id} className="flex items-center gap-2 text-sm">
                    <span className="font-medium">{a.file_name || a.file_id}</span>
                    <span className="text-xs text-muted-foreground">{formatDate(a.created_at)}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Timeline (V2) */}
          {isTeamV2 && events.length > 0 && (
            <div className="rounded-md border p-3">
              <p className="mb-2 text-xs font-medium text-muted-foreground">{t("tasks.detail.timeline")}</p>
              <div className="space-y-2">
                {events.map((e) => (
                  <div key={e.id} className="flex items-center gap-2 text-xs">
                    <span className="text-muted-foreground">{formatDate(e.created_at)}</span>
                    <Badge variant="outline" className="text-[10px]">{e.event_type}</Badge>
                    <span className="text-muted-foreground">
                      {e.actor_type === "human" ? "Human" : (resolveMember(e.actor_id) || e.actor_id.slice(0, 8))}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

        </div>

        {canDelete && (
          <div className="flex justify-end border-t pt-3">
            <Button variant="destructive" size="sm" onClick={() => setConfirmDelete(true)}>
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
              {t("tasks.delete")}
            </Button>
          </div>
        )}

        <ConfirmDialog
          open={confirmDelete}
          onOpenChange={setConfirmDelete}
          title={t("tasks.delete")}
          description={t("tasks.deleteConfirm")}
          confirmLabel={t("tasks.delete")}
          variant="destructive"
          onConfirm={handleDelete}
          loading={deleting}
        />
      </DialogContent>
    </Dialog>
  );
}
