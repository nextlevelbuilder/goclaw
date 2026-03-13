import { useState, useEffect, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { RefreshCw, Plus } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { useTranslation } from "react-i18next";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Events } from "@/api/protocol";
import { toast } from "@/stores/use-toast-store";
import type { TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment, ScopeEntry } from "@/types/team";
import type { TeamMemberData } from "@/types/team";
import { TaskList } from "./task-sections";

type StatusFilter = "" | "in_review" | "completed" | "all";

interface TeamTasksTabProps {
  teamId: string;
  members: TeamMemberData[];
  scopes?: ScopeEntry[];
  isTeamV2?: boolean;
  getTeamTasks: (teamId: string, status?: string, channel?: string, chatId?: string) => Promise<{ tasks: TeamTaskData[]; count: number }>;
  getTaskDetail: (teamId: string, taskId: string) => Promise<{
    task: TeamTaskData;
    comments: TeamTaskComment[];
    events: TeamTaskEvent[];
    attachments: TeamTaskAttachment[];
  }>;
  approveTask: (teamId: string, taskId: string, comment?: string) => Promise<void>;
  rejectTask: (teamId: string, taskId: string, reason?: string) => Promise<void>;
  addTaskComment: (taskId: string, content: string, teamId?: string) => Promise<void>;
  createTask: (teamId: string, params: { subject: string; description?: string; priority?: number; taskType?: string; assignTo?: string; channel?: string; chatId?: string }) => Promise<TeamTaskData>;
  assignTask: (teamId: string, taskId: string, agentId: string) => Promise<void>;
}

const v1Filters: { value: StatusFilter; labelKey: string }[] = [
  { value: "", labelKey: "tasks.filters.active" },
  { value: "completed", labelKey: "tasks.filters.completed" },
  { value: "all", labelKey: "tasks.filters.all" },
];

const v2Filters: { value: StatusFilter; labelKey: string }[] = [
  { value: "", labelKey: "tasks.filters.active" },
  { value: "in_review", labelKey: "tasks.filters.inReview" },
  { value: "completed", labelKey: "tasks.filters.completed" },
  { value: "all", labelKey: "tasks.filters.all" },
];

export function TeamTasksTab({
  teamId, members, scopes, isTeamV2,
  getTeamTasks,
  getTaskDetail, approveTask, rejectTask, addTaskComment,
  createTask, assignTask,
}: TeamTasksTabProps) {
  const { t } = useTranslation("teams");
  const [tasks, setTasks] = useState<TeamTaskData[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("");
  const [selectedScope, setSelectedScope] = useState<ScopeEntry | null>(null);
  const spinning = useMinLoading(loading);

  // Create dialog state
  const [createOpen, setCreateOpen] = useState(false);
  const [subject, setSubject] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState(0);
  const [taskType, setTaskType] = useState("general");
  const [assignTo, setAssignTo] = useState("");
  const [creating, setCreating] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getTeamTasks(teamId, statusFilter || undefined, selectedScope?.channel, selectedScope?.chat_id);
      setTasks(res.tasks ?? []);
    } catch (err) {
      console.error("[TeamTasksTab] load failed:", err);
    } finally {
      setLoading(false);
    }
  }, [teamId, getTeamTasks, statusFilter, selectedScope]);

  useEffect(() => {
    load();
  }, [load]);

  // Auto-reload task list on real-time team task events.
  const reloadOnEvent = useCallback((payload: unknown) => {
    const p = payload as { team_id?: string };
    if (!p?.team_id || p.team_id === teamId) load();
  }, [teamId, load]);
  useWsEvent(Events.TEAM_TASK_CREATED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_CLAIMED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_COMPLETED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_CANCELLED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_REVIEWED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_APPROVED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_REJECTED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_PROGRESS, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_ASSIGNED, reloadOnEvent);

  const handleOpenCreate = () => {
    setSubject("");
    setDescription("");
    setPriority(0);
    setTaskType("general");
    setAssignTo("");
    setCreateOpen(true);
  };

  const handleCreate = async () => {
    if (!subject.trim()) return;
    setCreating(true);
    try {
      await createTask(teamId, {
        subject: subject.trim(),
        description: description.trim() || undefined,
        priority,
        taskType: taskType || undefined,
        assignTo: assignTo || undefined,
        channel: selectedScope?.channel || undefined,
        chatId: selectedScope?.chat_id || undefined,
      });
      toast.success(t("toast.taskCreated"));
      setCreateOpen(false);
      load();
    } catch {
      toast.error(t("toast.failedCreateTask"));
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          {/* Status filter toggle */}
          <div className="flex rounded-lg border bg-muted/50 p-0.5">
          {(isTeamV2 ? v2Filters : v1Filters).map((f) => (
            <button
              key={f.value}
              onClick={() => setStatusFilter(f.value)}
              className={
                "rounded-md px-3 py-1 text-xs font-medium transition-colors " +
                (statusFilter === f.value
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground")
              }
            >
              {t(f.labelKey)}
            </button>
          ))}
          </div>
          {/* Scope filter */}
          {scopes && scopes.length > 0 && (
            <select
              value={selectedScope ? `${selectedScope.channel}:${selectedScope.chat_id}` : ""}
              onChange={(e) => {
                if (!e.target.value) {
                  setSelectedScope(null);
                } else {
                  const idx = e.target.value.indexOf(":");
                  setSelectedScope({ channel: e.target.value.slice(0, idx), chat_id: e.target.value.slice(idx + 1) });
                }
              }}
              className="rounded-md border bg-background px-2 py-1 text-base md:text-sm"
            >
              <option value="">{t("scope.all")}</option>
              {scopes.map((s) => (
                <option key={`${s.channel}:${s.chat_id}`} value={`${s.channel}:${s.chat_id}`}>
                  {s.channel}:{s.chat_id.length > 12 ? s.chat_id.slice(0, 12) + "…" : s.chat_id}
                </option>
              ))}
            </select>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleOpenCreate} className="gap-1">
            <Plus className="h-3.5 w-3.5" /> {t("tasks.createTask")}
          </Button>
          <Button variant="outline" size="sm" onClick={load} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {t("tasks.refresh")}
          </Button>
        </div>
      </div>

      <TaskList
        tasks={tasks}
        loading={loading}
        teamId={teamId}
        members={members}
        isTeamV2={isTeamV2}
        getTaskDetail={getTaskDetail}
        approveTask={approveTask}
        rejectTask={rejectTask}
        addTaskComment={addTaskComment}
        assignTask={assignTask}
      />

      {/* Create Task Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="w-[95vw] sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("tasks.createTaskTitle")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            {/* Subject */}
            <div className="space-y-1.5">
              <label className="text-sm font-medium">
                {t("tasks.columns.subject")} *
              </label>
              <input
                type="text"
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
                placeholder={t("tasks.subjectPlaceholder")}
                className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm"
                autoFocus
              />
            </div>

            {/* Description */}
            <div className="space-y-1.5">
              <label className="text-sm font-medium">
                {t("tasks.detail.description")}
              </label>
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t("tasks.descriptionPlaceholder")}
                rows={4}
                className="w-full resize-none rounded-md border bg-background px-3 py-2 text-base md:text-sm"
              />
            </div>

            {/* Task Type + Priority row */}
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <label className="text-sm font-medium">
                  {t("tasks.detail.type").replace(":", "")}
                </label>
                <select
                  value={taskType}
                  onChange={(e) => setTaskType(e.target.value)}
                  className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm"
                >
                  <option value="general">General</option>
                  <option value="delegation">Delegation</option>
                  <option value="escalation">Escalation</option>
                </select>
              </div>
              <div className="space-y-1.5">
                <label className="text-sm font-medium">
                  {t("tasks.priorityLabel")}
                </label>
                <input
                  type="number"
                  value={priority}
                  onChange={(e) => setPriority(Number(e.target.value))}
                  min={0}
                  className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm"
                />
              </div>
            </div>

            {/* Assign To */}
            {members.length > 0 && (
              <div className="space-y-1.5">
                <label className="text-sm font-medium">
                  {t("tasks.assignTo")}
                </label>
                <select
                  value={assignTo}
                  onChange={(e) => setAssignTo(e.target.value)}
                  className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm"
                >
                  <option value="">{t("tasks.unassigned")}</option>
                  {members.map((m) => (
                    <option key={m.agent_id} value={m.agent_id}>
                      {m.display_name || m.agent_key || m.agent_id}
                      {m.role === "lead" ? " (Lead)" : ""}
                    </option>
                  ))}
                </select>
              </div>
            )}
          </div>
          <DialogFooter className="gap-2">
            <Button variant="outline" size="sm" onClick={() => setCreateOpen(false)} disabled={creating}>
              {t("create.cancel")}
            </Button>
            <Button size="sm" onClick={handleCreate} disabled={creating || !subject.trim()}>
              {creating ? t("create.creating") : t("tasks.createTask")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
