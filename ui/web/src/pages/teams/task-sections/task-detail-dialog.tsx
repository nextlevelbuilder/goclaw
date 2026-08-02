import { useState, useEffect, useCallback, useRef } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import {
  Trash2, ArrowUp, ArrowDown, ArrowRight, AlertTriangle,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { formatDate } from "@/lib/format";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Events } from "@/api/protocol";
import { toast } from "@/stores/use-toast-store";
import type {
  TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment,
  TeamWorkflowDetailResponse, TeamWorkflowActionRequest, TeamWorkflowActionResponse,
  WorkflowAction,
} from "@/types/team";
import type { TeamTaskEventPayload, TeamWorkflowUpdatedPayload } from "@/types/team-events";
import { taskStatusBadgeVariant, isTerminalStatus } from "./task-utils";
import { TaskDetailContent } from "./task-detail-content";
import { TaskDetailAttachments } from "./task-detail-attachments";
import { TaskDetailComments } from "./task-detail-comments";
import { TaskDetailTimeline } from "./task-detail-timeline";
import { WorkflowActionDialog } from "./workflow-action-dialog";

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

/* ── Metadata item ────────────────────────────────────────────── */

function MetaItem({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground mb-0.5">{label}</dt>
      <dd className="font-medium">{children}</dd>
    </div>
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
  getWorkflow?: (teamId: string, workflowId: string) => Promise<TeamWorkflowDetailResponse>;
  applyWorkflowAction?: (params: TeamWorkflowActionRequest) => Promise<TeamWorkflowActionResponse>;
  onWorkflowChanged?: () => void;
  deleteTask?: (teamId: string, taskId: string) => Promise<void>;
  onAddComment?: (teamId: string, taskId: string, content: string) => Promise<void>;
  taskLookup?: Map<string, string>;
  memberLookup?: Map<string, string>;
  emojiLookup?: Map<string, string>;
  onNavigateTask?: (taskId: string) => void;
}

export function TaskDetailDialog({
  task, teamId, isTeamV2, onClose,
  getTaskDetail, getWorkflow, applyWorkflowAction, onWorkflowChanged,
  deleteTask, onAddComment, taskLookup, memberLookup, emojiLookup, onNavigateTask,
}: TaskDetailDialogProps) {
  const { t } = useTranslation("teams");
  const [events, setEvents] = useState<TeamTaskEvent[]>([]);
  const [attachments, setAttachments] = useState<TeamTaskAttachment[]>([]);
  const [comments, setComments] = useState<TeamTaskComment[]>([]);
  const [workflow, setWorkflow] = useState<TeamWorkflowDetailResponse | null>(null);
  const [workflowBusy, setWorkflowBusy] = useState(false);
  const [workflowAction, setWorkflowAction] = useState<WorkflowAction | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const loadDetail = useCallback(async () => {
    try {
      const res = await getTaskDetail(teamId, task.id);
      setEvents(res.events ?? []);
      setAttachments(res.attachments ?? []);
      setComments(res.comments ?? []);
    } catch { /* partial data acceptable */ }
  }, [getTaskDetail, teamId, task.id]);

  useEffect(() => { loadDetail(); }, [loadDetail]);

  const loadWorkflow = useCallback(async () => {
    if (!task.workflow_id || !getWorkflow) {
      setWorkflow(null);
      return;
    }
    try {
      setWorkflow(await getWorkflow(teamId, task.workflow_id));
    } catch { setWorkflow(null); }
  }, [getWorkflow, teamId, task.workflow_id]);

  useEffect(() => { loadWorkflow(); }, [loadWorkflow]);

  const onDetailEvent = useCallback((payload: unknown) => {
    const p = payload as TeamTaskEventPayload;
    if (p?.task_id === task.id) loadDetail();
  }, [task.id, loadDetail]);
  useWsEvent(Events.TEAM_TASK_COMMENTED, onDetailEvent);
  useWsEvent(Events.TEAM_TASK_ATTACHMENT_ADDED, onDetailEvent);

  const workflowRefetchTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(workflowRefetchTimer.current), []);
  const onWorkflowEvent = useCallback((payload: unknown) => {
    const p = payload as TeamWorkflowUpdatedPayload;
    if (p?.team_id !== teamId || p.workflow_id !== task.workflow_id) return;
    clearTimeout(workflowRefetchTimer.current);
    workflowRefetchTimer.current = setTimeout(loadWorkflow, 300);
  }, [teamId, task.workflow_id, loadWorkflow]);
  useWsEvent(Events.TEAM_WORKFLOW_UPDATED, onWorkflowEvent);

  const runWorkflowAction = async (reason: string) => {
    if (!workflow || !workflowAction || !applyWorkflowAction) return;
    const stepScoped = workflowAction === "retry_blocked";
    const blockedTask = workflow.tasks.find((item) =>
      item.plan_revision === workflow.workflow.plan_revision &&
      item.workflow_kind === "work" && item.status === "blocked"
    );
    if (stepScoped && !blockedTask) {
      toast.warning(t("workflow.toast.conflict"), t("workflow.toast.refreshed"));
      await loadWorkflow();
      setWorkflowAction(null);
      return;
    }
    setWorkflowBusy(true);
    try {
      const response = await applyWorkflowAction({
        teamId,
        workflowId: workflow.workflow.id,
        action: workflowAction,
        expectedStatus: workflow.workflow.status,
        expectedPlanRevision: workflow.workflow.plan_revision,
        ...(stepScoped ? {
          taskId: blockedTask!.id,
          expectedTaskStatus: blockedTask!.status,
        } : {}),
        reason,
      });
      // Reconcile in three steps for every outcome (applied / already_applied / conflict):
      // (1) replace local detail from the authoritative action response,
      // (2) refetch the authoritative workflow detail,
      // (3) refetch the board.
      setWorkflow(response);
      setWorkflowAction(null);
      if (response.outcome === "conflict") {
        toast.warning(t("workflow.toast.conflict"), t("workflow.toast.refreshed"));
      } else {
        toast.success(t(`workflow.toast.${response.outcome}`));
      }
      await loadWorkflow();
      await onWorkflowChanged?.();
    } catch (error) {
      toast.error(t("workflow.toast.error"), error instanceof Error ? error.message : undefined);
    } finally { setWorkflowBusy(false); }
  };

  const resolveMember = (id?: string) => (id && memberLookup?.get(id)) || undefined;
  const resolveEmoji = (id?: string) => (id && emojiLookup?.get(id)) || undefined;

  const handleDelete = async () => {
    if (!deleteTask) return;
    setDeleting(true);
    try { await deleteTask(teamId, task.id); onClose(); }
    catch { /* toast handled by hook */ }
    finally { setDeleting(false); setConfirmDelete(false); }
  };

  const ownerEmoji = resolveEmoji(task.owner_agent_id);
  const canDelete = deleteTask && isTerminalStatus(task.status);

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
              {t(`taskStatus.${task.status}`)}
            </Badge>
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

          {workflow && (() => {
            const wf = workflow.workflow;
            const blockedWorkTask = workflow.tasks.find((item) =>
              item.plan_revision === wf.plan_revision &&
              item.workflow_kind === "work" && item.status === "blocked"
            );
            return (
              <section className="space-y-3 rounded-lg border bg-muted/20 p-4">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-xs font-medium text-muted-foreground">{t("workflow.title")}</span>
                  <Badge variant="outline">{t(`workflow.status.${wf.status}`)}</Badge>
                  <Badge variant="outline">{t("workflow.revision", { revision: wf.plan_revision })}</Badge>
                  <span className="text-xs text-muted-foreground">
                    {wf.coordinator_display_name || wf.coordinator_agent_key}
                  </span>
                </div>

                {/* Expansion / delivery progress: distinct status + attempt counts */}
                <div className="grid grid-cols-1 gap-1 text-xs text-muted-foreground sm:grid-cols-3">
                  <span>{t("workflow.attempts.expansion", { count: wf.expansion_attempt_count })}</span>
                  <span>{t("workflow.attempts.delivery", { count: wf.delivery_attempt_count })}</span>
                  <span>{t("workflow.delivery.label", { status: t(`workflow.delivery.${wf.delivery_status}`) })}</span>
                </div>

                {/* Distinct reason lines — render each only when present */}
                {blockedWorkTask?.blocker_reason && (
                  <p className="text-xs text-muted-foreground">
                    <span className="font-medium">{t("workflow.reasons.blocker")}:</span> {blockedWorkTask.blocker_reason}
                  </p>
                )}
                {wf.last_expansion_error && (
                  <p className="text-xs text-destructive">
                    <span className="font-medium">{t("workflow.reasons.expansionError")}:</span> {wf.last_expansion_error}
                  </p>
                )}
                {wf.last_delivery_error && (
                  <p className="text-xs text-destructive">
                    <span className="font-medium">{t("workflow.reasons.deliveryError")}:</span> {wf.last_delivery_error}
                  </p>
                )}
                {wf.cancel_reason && (
                  <p className="text-xs text-muted-foreground">
                    <span className="font-medium">{t("workflow.reasons.cancellation")}:</span> {wf.cancel_reason}
                  </p>
                )}
                {wf.failure_summary && (
                  <p className="text-xs text-destructive">
                    <span className="font-medium">{t("workflow.reasons.failure")}:</span> {wf.failure_summary}
                  </p>
                )}

                <div className="flex flex-wrap gap-2">
                  {workflow.allowed_actions.map((action) => (
                    <Button
                      key={action}
                      size="sm"
                      variant={action === "cancel_workflow" || action === "fail_workflow" ? "destructive" : "outline"}
                      disabled={workflowBusy}
                      onClick={() => setWorkflowAction(action)}
                    >
                      {t(`workflow.actions.${action}.label`)}
                    </Button>
                  ))}
                </div>
              </section>
            );
          })()}

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
            {task.workflow_step_id && (
              <MetaItem label={t("workflow.step")}>
                {task.workflow_step_id} · {t("workflow.revision", { revision: task.plan_revision ?? 1 })}
              </MetaItem>
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
        {canDelete && (
          <div className="flex justify-end border-t pt-3">
            <Button variant="destructive" size="sm" onClick={() => setConfirmDelete(true)}>
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
              {t("tasks.delete")}
            </Button>
          </div>
        )}

        <WorkflowActionDialog
          action={workflowAction}
          loading={workflowBusy}
          onOpenChange={(open) => { if (!open && !workflowBusy) setWorkflowAction(null); }}
          onConfirm={runWorkflowAction}
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
