import { useState, useEffect, useCallback } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { Trash2, Paperclip } from "lucide-react";
import { useTranslation } from "react-i18next";
import { formatDate, formatFileSize } from "@/lib/format";
import { useAuthStore } from "@/stores/use-auth-store";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Events } from "@/api/protocol";
import type { TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment } from "@/types/team";
import type { TeamTaskEventPayload } from "@/types/team-events";
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
  onAddComment?: (teamId: string, taskId: string, content: string) => Promise<void>;
  taskLookup?: Map<string, string>;
  memberLookup?: Map<string, string>;
  emojiLookup?: Map<string, string>;
  onNavigateTask?: (taskId: string) => void;
}

export function TaskDetailDialog({
  task, teamId, isTeamV2, onClose,
  getTaskDetail, deleteTask, onAddComment, taskLookup, memberLookup, emojiLookup, onNavigateTask,
}: TaskDetailDialogProps) {
  const { t } = useTranslation("teams");
  const token = useAuthStore((s) => s.token);
  const [events, setEvents] = useState<TeamTaskEvent[]>([]);
  const [attachments, setAttachments] = useState<TeamTaskAttachment[]>([]);
  const [comments, setComments] = useState<TeamTaskComment[]>([]);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [newComment, setNewComment] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const loadDetail = useCallback(async () => {
    try {
      const res = await getTaskDetail(teamId, task.id);
      setEvents(res.events ?? []);
      setAttachments(res.attachments ?? []);
      setComments(res.comments ?? []);
    } catch { /* partial data acceptable */ }
  }, [getTaskDetail, teamId, task.id]);

  useEffect(() => { loadDetail(); }, [loadDetail]);

  // Auto-refresh when a comment is added to this task (by another user/agent).
  const onCommentEvent = useCallback((payload: unknown) => {
    const p = payload as TeamTaskEventPayload;
    if (p?.task_id === task.id) loadDetail();
  }, [task.id, loadDetail]);
  useWsEvent(Events.TEAM_TASK_COMMENTED, onCommentEvent);

  const resolveMember = (id?: string) =>
    (id && memberLookup?.get(id)) || undefined;

  const resolveEmoji = (id?: string) =>
    (id && emojiLookup?.get(id)) || undefined;

  const handleDelete = async () => {
    if (!deleteTask) return;
    setDeleting(true);
    try {
      await deleteTask(teamId, task.id);
      onClose();
    } catch {
      // toast handled by hook
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
              <p className="mb-2 text-xs font-medium text-muted-foreground">{t("tasks.detail.attachments")} ({attachments.length})</p>
              <div className="space-y-1">
                {attachments.map((a) => (
                  <div key={a.id} className="flex items-center justify-between gap-2 text-sm">
                    <div className="flex items-center gap-2 min-w-0">
                      <Paperclip className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                      <span className="truncate font-medium">{a.path?.split("/").pop() || a.path}</span>
                      {a.file_size > 0 && (
                        <span className="text-xs text-muted-foreground">{formatFileSize(a.file_size)}</span>
                      )}
                    </div>
                    <a
                      href={`/v1/teams/${teamId}/attachments/${a.id}/download?token=${encodeURIComponent(token)}`}
                      download
                      className="shrink-0 text-xs text-primary hover:underline"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {t("tasks.detail.download")}
                    </a>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Comments (V2) */}
          {isTeamV2 && comments.length > 0 && (
            <div className="rounded-md border p-3">
              <p className="mb-2 text-xs font-medium text-muted-foreground">{t("tasks.detail.comments")} ({comments.length})</p>
              <div className="space-y-2">
                {comments.map((c) => (
                  <div key={c.id} className="text-sm">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <span className="font-medium text-foreground">
                        {c.agent_key || (c.user_id ? "User" : "Unknown")}
                      </span>
                      <span>{formatDate(c.created_at)}</span>
                    </div>
                    <p className="mt-0.5 whitespace-pre-wrap">{c.content}</p>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Add Comment (V2) */}
          {isTeamV2 && onAddComment && (
            <div className="rounded-md border p-3">
              <p className="mb-2 text-xs font-medium text-muted-foreground">{t("tasks.detail.addComment")}</p>
              <div className="flex gap-2">
                <textarea
                  className="min-h-[60px] flex-1 resize-none rounded-md border bg-background px-3 py-2 text-base md:text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
                  placeholder={t("tasks.detail.commentPlaceholder")}
                  value={newComment}
                  onChange={(e) => setNewComment(e.target.value)}
                  disabled={submitting}
                />
                <Button
                  size="sm"
                  className="self-end"
                  disabled={submitting || newComment.trim() === ""}
                  onClick={async () => {
                    if (!newComment.trim()) return;
                    setSubmitting(true);
                    try {
                      await onAddComment(teamId, task.id, newComment.trim());
                      setNewComment("");
                      await loadDetail();
                    } catch {
                      /* error handled by caller */
                    } finally {
                      setSubmitting(false);
                    }
                  }}
                >
                  {t("tasks.detail.addComment")}
                </Button>
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
