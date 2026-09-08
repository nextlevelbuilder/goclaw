import { useState, useEffect, useCallback } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import {
  Trash2, ArrowUp, ArrowDown, ArrowRight, AlertTriangle, Copy, Check, Ban, RotateCcw,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { formatDate } from "@/lib/format";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Events } from "@/api/protocol";
import type { TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment } from "@/types/team";
import type { TeamTaskEventPayload } from "@/types/team-events";
import { taskStatusBadgeVariant, isTerminalStatus, canCancelTask, canRetryTask } from "./task-utils";
import { TaskDetailContent } from "./task-detail-content";
import { TaskDetailAttachments } from "./task-detail-attachments";
import { TaskDetailComments } from "./task-detail-comments";
import { TaskDetailTimeline } from "./task-detail-timeline";

/* ── Priority helpers (numeric: 1=low … 4=critical) ───────────── */

const PRIORITY_CONFIG: Record<number, { icon: typeof ArrowUp; color: string; label: string }> = {
  4: { icon: AlertTriangle, color: "text-red-500", label: "critical" },
  3: { icon: ArrowUp, color: "text-orange-500", label: "high" },
  2: { icon: ArrowRight, color: "text-yellow-500", label: "medium" },
  1: { icon: ArrowDown, color: "text-muted-foreground", label: "low" },
};

function PriorityIcon({ priority }: { priority?: number }) {
  const cfg = priority != null ? PRIORITY_CONFIG[priority] : undefined;
  if (!cfg) return null;
  const Icon = cfg.icon;
  return <Icon className={`h-3.5 w-3.5 ${cfg.color}`} />;
}

function priorityLabel(priority?: number) {
  return priority != null ? (PRIORITY_CONFIG[priority]?.label ?? String(priority)) : "\u2014";
}

/* ── Task UUID with copy ──────────────────────────────────────── */

// The short identifier (T-015-cc8e) carries only the last four hex chars of
// the UUID, while every agent-facing tool (team_tasks get/retry/cancel, comments)
// takes the full UUID. Show it in full and make it one click to copy so a
// human can hand the exact task to the lead in chat.
function TaskIdCopy({ id, label, copiedLabel }: { id: string; label: string; copiedLabel: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
    } catch { /* clipboard unavailable (insecure context) — the text is still selectable */ }
  };

  return (
    <button
      type="button"
      onClick={copy}
      title={copied ? copiedLabel : label}
      className="inline-flex items-center gap-1 rounded px-1 font-mono text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground select-all"
    >
      <span className="truncate max-w-[16rem] sm:max-w-none">{id}</span>
      {copied
        ? <Check className="h-3 w-3 shrink-0 text-green-600" />
        : <Copy className="h-3 w-3 shrink-0" />}
    </button>
  );
}

/* ── Metadata item ────────────────────────────────────────────── */

function MetaItem({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground mb-0.5">{label}</dt>
      <dd className="font-medium">{children}</dd>
    </div>
  );
}

/* ── Action dialog (cancel with optional reason / retry with required comment) ── */

export interface TaskAssigneeOption { id: string; name: string }

interface TaskActionDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  placeholder: string;
  confirmLabel: string;
  required?: boolean;
  requiredHint?: string;
  variant?: "default" | "destructive";
  loading?: boolean;
  /** When set, an assignee select is shown above the textarea (retry). */
  assignees?: TaskAssigneeOption[];
  assigneeLabel?: string;
  defaultAssigneeId?: string;
  onConfirm: (text: string, assigneeId?: string) => void;
}

function TaskActionDialog({
  open, onOpenChange, title, description, placeholder, confirmLabel,
  required, requiredHint, variant = "default", loading,
  assignees, assigneeLabel, defaultAssigneeId, onConfirm,
}: TaskActionDialogProps) {
  const { t } = useTranslation("common");
  const [text, setText] = useState("");
  const [assigneeId, setAssigneeId] = useState<string>("");
  useEffect(() => {
    if (open) { setText(""); setAssigneeId(defaultAssigneeId ?? ""); }
  }, [open, defaultAssigneeId]);
  const withAssignee = !!assignees && assignees.length > 0;
  const missing = (!!required && text.trim() === "") || (withAssignee && assigneeId === "");

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!loading) onOpenChange(v); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">{description}</p>
        {withAssignee && (
          <div className="space-y-1">
            {assigneeLabel && <p className="text-xs text-muted-foreground">{assigneeLabel}</p>}
            <Select value={assigneeId} onValueChange={setAssigneeId} disabled={loading}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {assignees!.map((a) => (
                  <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        <Textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={placeholder}
          rows={4}
          autoFocus
          disabled={loading}
        />
        {missing && requiredHint && (
          <p className="text-xs text-muted-foreground">{requiredHint}</p>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={loading}>
            {t("cancel")}
          </Button>
          <Button variant={variant} size="sm" onClick={() => onConfirm(text.trim(), assigneeId || undefined)} disabled={loading || missing}>
            {loading ? "..." : confirmLabel}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/* ── Props ────────────────────────────────────────────────────── */

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
  cancelTask?: (teamId: string, taskId: string, reason?: string) => Promise<void>;
  retryTask?: (teamId: string, taskId: string, comment: string, agentId?: string) => Promise<void>;
  /** Team members a retried task may be (re)assigned to; the lead is excluded. */
  assignees?: TaskAssigneeOption[];
  onAddComment?: (teamId: string, taskId: string, content: string) => Promise<void>;
  taskLookup?: Map<string, string>;
  memberLookup?: Map<string, string>;
  emojiLookup?: Map<string, string>;
  onNavigateTask?: (taskId: string) => void;
}

export function TaskDetailDialog({
  task, teamId, isTeamV2, onClose,
  getTaskDetail, deleteTask, cancelTask, retryTask, assignees, onAddComment, taskLookup, memberLookup, emojiLookup, onNavigateTask,
}: TaskDetailDialogProps) {
  const { t } = useTranslation("teams");
  const [events, setEvents] = useState<TeamTaskEvent[]>([]);
  const [attachments, setAttachments] = useState<TeamTaskAttachment[]>([]);
  const [comments, setComments] = useState<TeamTaskComment[]>([]);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [retryOpen, setRetryOpen] = useState(false);
  const [retrying, setRetrying] = useState(false);

  const loadDetail = useCallback(async () => {
    try {
      const res = await getTaskDetail(teamId, task.id);
      setEvents(res.events ?? []);
      setAttachments(res.attachments ?? []);
      setComments(res.comments ?? []);
    } catch { /* partial data acceptable */ }
  }, [getTaskDetail, teamId, task.id]);

  useEffect(() => { loadDetail(); }, [loadDetail]);

  const onCommentEvent = useCallback((payload: unknown) => {
    const p = payload as TeamTaskEventPayload;
    if (p?.task_id === task.id) loadDetail();
  }, [task.id, loadDetail]);
  useWsEvent(Events.TEAM_TASK_COMMENTED, onCommentEvent);

  const resolveMember = (id?: string) => (id && memberLookup?.get(id)) || undefined;
  const resolveEmoji = (id?: string) => (id && emojiLookup?.get(id)) || undefined;

  const handleDelete = async () => {
    if (!deleteTask) return;
    setDeleting(true);
    try { await deleteTask(teamId, task.id); onClose(); }
    catch { /* toast handled by hook */ }
    finally { setDeleting(false); setConfirmDelete(false); }
  };

  const handleCancel = async (reason: string) => {
    if (!cancelTask) return;
    setCancelling(true);
    try { await cancelTask(teamId, task.id, reason || undefined); onClose(); }
    catch { /* toast handled by hook */ }
    finally { setCancelling(false); setCancelOpen(false); }
  };

  const handleRetry = async (comment: string, assigneeId?: string) => {
    if (!retryTask || !comment) return;
    // Only send agentId when it actually changes the assignee; the backend
    // keeps the current owner otherwise.
    const agentId = assigneeId && assigneeId !== task.owner_agent_id ? assigneeId : undefined;
    if (!task.owner_agent_id && !agentId) return;
    setRetrying(true);
    try { await retryTask(teamId, task.id, comment, agentId); onClose(); }
    catch { /* toast handled by hook */ }
    finally { setRetrying(false); setRetryOpen(false); }
  };

  const ownerEmoji = resolveEmoji(task.owner_agent_id);
  const canDelete = deleteTask && isTerminalStatus(task.status);
  const canCancel = !!cancelTask && canCancelTask(task.status);
  // Without an owner the backend needs agentId, so retry is offered only when
  // there is someone to pick.
  const canRetry = !!retryTask && canRetryTask(task) && (!!task.owner_agent_id || (assignees?.length ?? 0) > 0);

  const handleAddComment = onAddComment
    ? async (content: string) => { await onAddComment(teamId, task.id, content); await loadDetail(); }
    : undefined;

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] w-[95vw] flex flex-col sm:max-w-4xl">
        {/* ── Header: badges + subject as title ── */}
        <DialogHeader>
          <div className="flex items-center gap-2 mb-1">
            {task.identifier && (
              <Badge variant="outline" className="font-mono text-xs">{task.identifier}</Badge>
            )}
            <Badge variant={taskStatusBadgeVariant(task.status)} className="text-xs">
              {task.status.replace(/_/g, " ")}
            </Badge>
            <TaskIdCopy id={task.id} label={t("tasks.detail.copyId")} copiedLabel={t("tasks.detail.copied")} />
          </div>
          <DialogTitle className="text-base sm:text-lg">{task.subject}</DialogTitle>
        </DialogHeader>

        {/* ── Scrollable body ── */}
        <div className="space-y-4 overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6">

          {/* Progress bar (V2) */}
          {isTeamV2 && task.progress_percent != null && task.progress_percent > 0 && !isTerminalStatus(task.status) && (() => {
            const pct = Math.min(100, Math.max(0, task.progress_percent));
            return (
              <div className="space-y-1">
                <div className="flex justify-between text-xs text-muted-foreground">
                  <span>{t("tasks.detail.progress")}</span>
                  <span>{pct}%</span>
                </div>
                <div className="h-2 w-full rounded-full bg-muted">
                  <div className="h-2 rounded-full bg-primary transition-all" style={{ width: `${pct}%` }} />
                </div>
                {task.progress_step && <p className="text-xs text-muted-foreground">{task.progress_step}</p>}
              </div>
            );
          })()}

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

          {/* Metadata grid */}
          <dl className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-x-6 gap-y-3 rounded-lg bg-muted/30 p-4 text-sm">
            <MetaItem label={t("tasks.detail.priority")}>
              <span className="flex items-center gap-1.5">
                <PriorityIcon priority={task.priority} />
                <span className="capitalize">{priorityLabel(task.priority)}</span>
              </span>
            </MetaItem>
            <MetaItem label={t("tasks.detail.owner")}>
              {ownerEmoji && <span className="mr-1">{ownerEmoji}</span>}
              {resolveMember(task.owner_agent_id) || task.owner_agent_key || "\u2014"}
            </MetaItem>
            {task.task_type && task.task_type !== "general" && (
              <MetaItem label={t("tasks.detail.type")}>
                <Badge variant="outline" className="text-xs">{task.task_type}</Badge>
              </MetaItem>
            )}
            {task.created_at && (
              <MetaItem label={t("tasks.detail.created")}>{formatDate(task.created_at)}</MetaItem>
            )}
            {task.updated_at && (
              <MetaItem label={t("tasks.detail.updated")}>{formatDate(task.updated_at)}</MetaItem>
            )}
          </dl>

          {/* Blocked by */}
          {task.blocked_by && task.blocked_by.length > 0 && (
            <div className="text-sm">
              <span className="text-muted-foreground">{t("tasks.detail.blockedBy")}</span>
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

          <Separator />

          {/* Content sections */}
          <TaskDetailContent description={task.description} result={task.result} />

          {isTeamV2 && <TaskDetailAttachments attachments={attachments} />}

          {isTeamV2 && (
            <TaskDetailComments comments={comments} onAddComment={handleAddComment} />
          )}

          {isTeamV2 && (
            <TaskDetailTimeline events={events} resolveMember={resolveMember} />
          )}
        </div>

        {/* Footer */}
        {(canRetry || canCancel || canDelete) && (
          <div className="flex flex-wrap justify-end gap-2 border-t pt-3">
            {canRetry && (
              <Button variant="default" size="sm" onClick={() => setRetryOpen(true)}>
                <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
                {t("tasks.retry")}
              </Button>
            )}
            {canCancel && (
              <Button variant="outline" size="sm" onClick={() => setCancelOpen(true)}>
                <Ban className="mr-1.5 h-3.5 w-3.5" />
                {t("tasks.cancel")}
              </Button>
            )}
            {canDelete && (
              <Button variant="destructive" size="sm" onClick={() => setConfirmDelete(true)}>
                <Trash2 className="mr-1.5 h-3.5 w-3.5" />
                {t("tasks.delete")}
              </Button>
            )}
          </div>
        )}

        <TaskActionDialog
          open={retryOpen}
          onOpenChange={setRetryOpen}
          title={t("tasks.retryTitle")}
          description={t("tasks.retryDescription")}
          placeholder={t("tasks.retryCommentPlaceholder")}
          confirmLabel={t("tasks.retry")}
          required
          requiredHint={t("tasks.retryCommentRequired")}
          assignees={assignees}
          assigneeLabel={t("tasks.retryAssignee")}
          defaultAssigneeId={task.owner_agent_id}
          loading={retrying}
          onConfirm={handleRetry}
        />

        <TaskActionDialog
          open={cancelOpen}
          onOpenChange={setCancelOpen}
          title={t("tasks.cancelTitle")}
          description={t("tasks.cancelDescription")}
          placeholder={t("tasks.cancelReasonPlaceholder")}
          confirmLabel={t("tasks.cancel")}
          variant="destructive"
          loading={cancelling}
          onConfirm={handleCancel}
        />

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
