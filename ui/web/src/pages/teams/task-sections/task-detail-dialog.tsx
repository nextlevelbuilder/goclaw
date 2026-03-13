import { useState, useEffect, useCallback } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useTranslation } from "react-i18next";
import { toast } from "@/stores/use-toast-store";
import { formatDate } from "@/lib/format";
import type { TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment } from "@/types/team";
import type { TeamMemberData } from "@/types/team";
import { taskStatusBadgeVariant } from "./task-utils";

interface TaskDetailDialogProps {
  task: TeamTaskData;
  teamId: string;
  members: TeamMemberData[];
  isTeamV2?: boolean;
  onClose: () => void;
  getTaskDetail: (teamId: string, taskId: string) => Promise<{
    task: TeamTaskData;
    comments: TeamTaskComment[];
    events: TeamTaskEvent[];
    attachments: TeamTaskAttachment[];
  }>;
  approveTask: (teamId: string, taskId: string, comment?: string) => Promise<void>;
  rejectTask: (teamId: string, taskId: string, reason?: string) => Promise<void>;
  addTaskComment: (taskId: string, content: string, teamId?: string) => Promise<void>;
  assignTask: (teamId: string, taskId: string, agentId: string) => Promise<void>;
}

export function TaskDetailDialog({
  task, teamId, members, isTeamV2, onClose,
  getTaskDetail, approveTask, rejectTask, addTaskComment, assignTask,
}: TaskDetailDialogProps) {
  const { t } = useTranslation("teams");
  const [comments, setComments] = useState<TeamTaskComment[]>([]);
  const [events, setEvents] = useState<TeamTaskEvent[]>([]);
  const [attachments, setAttachments] = useState<TeamTaskAttachment[]>([]);
  const [commentText, setCommentText] = useState("");
  const [submitting, setSubmitting] = useState(false);

  // Assign state
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [assigning, setAssigning] = useState(false);

  const loadDetail = useCallback(async () => {
    try {
      const res = await getTaskDetail(teamId, task.id);
      setComments(res.comments ?? []);
      setEvents(res.events ?? []);
      setAttachments(res.attachments ?? []);
    } catch {
      // ignore load errors — partial data is acceptable
    }
  }, [getTaskDetail, teamId, task.id]);

  useEffect(() => {
    loadDetail();
  }, [loadDetail]);

  const handleApprove = async () => {
    setSubmitting(true);
    try {
      await approveTask(teamId, task.id);
      onClose();
    } catch {
      toast.error(t("toast.failedUpdate"));
    } finally {
      setSubmitting(false);
    }
  };

  const handleReject = async () => {
    setSubmitting(true);
    try {
      await rejectTask(teamId, task.id, "Rejected from dashboard");
      onClose();
    } catch {
      toast.error(t("toast.failedUpdate"));
    } finally {
      setSubmitting(false);
    }
  };

  const handleComment = async () => {
    if (!commentText.trim()) return;
    setSubmitting(true);
    try {
      await addTaskComment(task.id, commentText.trim(), teamId);
      setCommentText("");
      loadDetail();
    } catch {
      toast.error(t("toast.failedUpdate"));
    } finally {
      setSubmitting(false);
    }
  };

  const handleAssign = async () => {
    if (!selectedAgentId) return;
    setAssigning(true);
    try {
      await assignTask(teamId, task.id, selectedAgentId);
      toast.success(t("toast.taskAssigned"));
      onClose();
    } catch {
      toast.error(t("toast.failedAssignTask"));
    } finally {
      setAssigning(false);
    }
  };

  const showAssign = task.status === "pending" && !task.owner_agent_key;

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] w-[95vw] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {task.identifier && (
              <Badge variant="outline" className="font-mono text-xs">{task.identifier}</Badge>
            )}
            {t("tasks.detail.title")}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* Subject */}
          <div className="rounded-md border p-3">
            <p className="mb-1 text-xs font-medium text-muted-foreground">{t("tasks.detail.subject")}</p>
            <p className="text-sm font-medium">{task.subject}</p>
          </div>

          {/* Follow-up status banner (V2 only) */}
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

          {/* Progress bar (V2 only) */}
          {isTeamV2 && task.progress_percent != null && task.progress_percent > 0 && (
            <div className="space-y-1">
              <div className="flex justify-between text-xs text-muted-foreground">
                <span>{t("tasks.detail.progress")}</span>
                <span>{task.progress_percent}%</span>
              </div>
              <div className="h-2 w-full rounded-full bg-muted">
                <div
                  className="h-2 rounded-full bg-primary transition-all"
                  style={{ width: `${task.progress_percent}%` }}
                />
              </div>
              {task.progress_step && (
                <p className="text-xs text-muted-foreground">{task.progress_step}</p>
              )}
            </div>
          )}

          {/* Summary grid */}
          <div className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
            <div>
              <span className="text-muted-foreground">{t("tasks.detail.status")}</span>{" "}
              <Badge variant={taskStatusBadgeVariant(task.status)} className="text-xs">
                {task.status.replace(/_/g, " ")}
              </Badge>
            </div>
            <div>
              <span className="text-muted-foreground">{t("tasks.detail.priority")}</span>{" "}
              <span className="font-medium">{task.priority}</span>
            </div>
            <div>
              <span className="text-muted-foreground">{t("tasks.detail.owner")}</span>{" "}
              <span className="font-medium">{task.owner_agent_key || "—"}</span>
            </div>
            {task.task_type && task.task_type !== "general" && (
              <div>
                <span className="text-muted-foreground">{t("tasks.detail.type")}</span>{" "}
                <Badge variant="outline" className="text-xs">{task.task_type}</Badge>
              </div>
            )}
            {task.created_at && (
              <div>
                <span className="text-muted-foreground">{t("tasks.detail.created")}</span>{" "}
                {formatDate(task.created_at)}
              </div>
            )}
            {task.updated_at && (
              <div>
                <span className="text-muted-foreground">{t("tasks.detail.updated")}</span>{" "}
                {formatDate(task.updated_at)}
              </div>
            )}
          </div>

          {/* Approve / Reject buttons (V2 only) */}
          {isTeamV2 && task.status === "in_review" && (
            <div className="flex gap-2">
              <Button
                variant="default"
                size="sm"
                onClick={handleApprove}
                disabled={submitting}
              >
                {t("tasks.detail.approve")}
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={handleReject}
                disabled={submitting}
              >
                {t("tasks.detail.reject")}
              </Button>
            </div>
          )}

          {/* Assign to Agent section */}
          {showAssign && members.length > 0 && (
            <div className="rounded-md border p-3">
              <p className="mb-2 text-xs font-medium text-muted-foreground">{t("tasks.assign")}</p>
              <div className="flex gap-2">
                <select
                  value={selectedAgentId}
                  onChange={(e) => setSelectedAgentId(e.target.value)}
                  className="flex-1 rounded-md border bg-background px-3 py-2 text-base md:text-sm"
                  disabled={assigning}
                >
                  <option value="">{t("tasks.assignTo")}</option>
                  {members.map((m) => (
                    <option key={m.agent_id} value={m.agent_id}>
                      {m.display_name || m.agent_key || m.agent_id}
                    </option>
                  ))}
                </select>
                <Button
                  size="sm"
                  onClick={handleAssign}
                  disabled={assigning || !selectedAgentId}
                >
                  {t("tasks.assign")}
                </Button>
              </div>
            </div>
          )}

          {/* Blocked by */}
          {task.blocked_by && task.blocked_by.length > 0 && (
            <div className="text-sm">
              <span className="text-muted-foreground">{t("tasks.detail.blockedBy")}</span>{" "}
              <span className="font-mono text-xs">{task.blocked_by.join(", ")}</span>
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
              <pre className="max-h-[40vh] overflow-y-auto whitespace-pre-wrap break-words text-sm">
                {task.result}
              </pre>
            </div>
          )}

          {/* Attachments (V2 only) */}
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

          {/* Timeline (V2 only) */}
          {isTeamV2 && events.length > 0 && (
            <div className="rounded-md border p-3">
              <p className="mb-2 text-xs font-medium text-muted-foreground">{t("tasks.detail.timeline")}</p>
              <div className="space-y-2">
                {events.map((e) => (
                  <div key={e.id} className="flex items-center gap-2 text-xs">
                    <span className="text-muted-foreground">{formatDate(e.created_at)}</span>
                    <Badge variant="outline" className="text-[10px]">{e.event_type}</Badge>
                    <span className="text-muted-foreground">
                      {e.actor_type === "human" ? "Human" : e.actor_id.slice(0, 8)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Comments (V2 only) */}
          {isTeamV2 && <div className="rounded-md border p-3">
            <p className="mb-2 text-xs font-medium text-muted-foreground">{t("tasks.detail.comments")}</p>
            {comments.length > 0 ? (
              <div className="mb-3 space-y-2">
                {comments.map((c) => (
                  <div key={c.id} className="border-l-2 border-muted pl-3">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <span className="font-medium">{c.agent_key || c.user_id || "—"}</span>
                      <span>{formatDate(c.created_at)}</span>
                    </div>
                    <p className="text-sm">{c.content}</p>
                  </div>
                ))}
              </div>
            ) : (
              <p className="mb-3 text-xs text-muted-foreground">{t("tasks.detail.noComments")}</p>
            )}

            {/* Comment input */}
            <div className="flex gap-2">
              <input
                type="text"
                value={commentText}
                onChange={(e) => setCommentText(e.target.value)}
                placeholder={t("tasks.detail.commentPlaceholder")}
                className="flex-1 rounded-md border bg-background px-3 py-1.5 text-base md:text-sm"
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    handleComment();
                  }
                }}
              />
              <Button
                variant="outline"
                size="sm"
                onClick={handleComment}
                disabled={submitting || !commentText.trim()}
              >
                {t("tasks.detail.addComment")}
              </Button>
            </div>
          </div>}
        </div>
      </DialogContent>
    </Dialog>
  );
}
