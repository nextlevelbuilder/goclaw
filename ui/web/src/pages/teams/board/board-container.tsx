import { useState, useEffect, useCallback, useMemo, useRef, memo } from "react";
import { useTranslation } from "react-i18next";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Events } from "@/api/protocol";
import { useBoardStore } from "../stores/use-board-store";
import { toast } from "@/stores/use-toast-store";
import { buildTaskLookup, buildMemberLookup, buildEmojiLookup } from "./board-utils";
import { BoardToolbar } from "./board-toolbar";
import { KanbanBoard } from "./kanban-board";
import { TaskDetailDialog } from "../task-sections/task-detail-dialog";
import { TaskList } from "../task-sections";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import type {
  TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment,
  TeamMemberData, ScopeEntry,
} from "@/types/team";

type StatusFilter = "all" | "pending" | "in_progress" | "completed";

/** Payload shape broadcast by team task WS events. */
interface TaskEventPayload {
  team_id?: string;
  task_id?: string;
  status?: string;
  progress_percent?: number;
  progress_step?: string;
  owner_agent_key?: string;
  channel?: string;
  chat_id?: string;
}

interface BoardContainerProps {
  teamId: string;
  members: TeamMemberData[];
  scopes: ScopeEntry[];
  isTeamV2: boolean;
  getTeamTasks: (teamId: string, status?: string, channel?: string, chatId?: string) => Promise<{ tasks: TeamTaskData[]; count: number }>;
  getTaskDetail: (teamId: string, taskId: string) => Promise<{ task: TeamTaskData; comments: TeamTaskComment[]; events: TeamTaskEvent[]; attachments: TeamTaskAttachment[] }>;
  getTaskLight: (teamId: string, taskId: string) => Promise<TeamTaskData>;
  deleteTask?: (teamId: string, taskId: string) => Promise<void>;
  deleteTasksBulk?: (teamId: string, taskIds: string[]) => Promise<number>;
  addTaskComment?: (teamId: string, taskId: string, content: string) => Promise<void>;
  onWorkspace?: () => void;
}

/** Check if a task matches the current filters. */
function taskMatchesFilter(task: TeamTaskData, sf: StatusFilter, scope: ScopeEntry | null): boolean {
  // Status filter
  switch (sf) {
    case "pending": if (task.status !== "pending") return false; break;
    case "in_progress": if (task.status !== "in_progress") return false; break;
    case "completed": if (task.status !== "completed" && task.status !== "cancelled") return false; break;
    // "all" → no status filter
  }
  // Scope filter
  if (scope) {
    if (scope.channel && (task.channel ?? "") !== scope.channel) return false;
    if (scope.chat_id && (task.chat_id ?? "") !== scope.chat_id) return false;
  }
  return true;
}

export const BoardContainer = memo(function BoardContainer({
  teamId, members, scopes, isTeamV2,
  getTeamTasks, getTaskDetail, getTaskLight, deleteTask, deleteTasksBulk, addTaskComment, onWorkspace,
}: BoardContainerProps) {
  const { t } = useTranslation("teams");
  const viewMode = useBoardStore((s) => s.viewMode);
  const groupBy = useBoardStore((s) => s.groupBy);

  const [tasks, setTasks] = useState<TeamTaskData[]>([]);
  const [initialized, setInitialized] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [selectedScope, setSelectedScope] = useState<ScopeEntry | null>(null);
  const [selectedTask, setSelectedTask] = useState<TeamTaskData | null>(null);
  const [deleteTargetId, setDeleteTargetId] = useState<string | null>(null);
  const [singleDeleting, setSingleDeleting] = useState(false);

  // Lookups for name resolution
  const taskLookup = useMemo(() => buildTaskLookup(tasks), [tasks]);
  const memberLookup = useMemo(() => buildMemberLookup(members), [members]);
  const emojiLookup = useMemo(() => buildEmojiLookup(members), [members]);

  // Stable refs for filter values — avoids recreating load callback
  const filtersRef = useRef({ statusFilter, selectedScope });
  filtersRef.current = { statusFilter, selectedScope };
  const getTeamTasksRef = useRef(getTeamTasks);
  getTeamTasksRef.current = getTeamTasks;
  const getTaskLightRef = useRef(getTaskLight);
  getTaskLightRef.current = getTaskLight;

  // ── Full reload (initial load, filter change, manual refresh) ──

  const load = useCallback(async (showSpinner = false) => {
    if (showSpinner) setRefreshing(true);
    try {
      const { statusFilter: sf, selectedScope: ss } = filtersRef.current;
      const backendFilter = (sf === "pending" || sf === "in_progress") ? "all" : sf;
      const res = await getTeamTasksRef.current(teamId, backendFilter, ss?.channel, ss?.chat_id);
      let result = res.tasks ?? [];
      if (sf === "pending") result = result.filter((t) => t.status === "pending");
      else if (sf === "in_progress") result = result.filter((t) => t.status === "in_progress");
      setTasks(result);
      setInitialized(true);
    } catch (err) {
      console.error("[BoardContainer] load failed:", err);
    } finally {
      if (showSpinner) setRefreshing(false);
    }
  }, [teamId]);

  useEffect(() => { load(); }, [load]);

  // Re-fetch when filters change
  useEffect(() => {
    if (initialized) load();
  }, [statusFilter, selectedScope]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── Delta updates via WS events ──

  // Per-task debounce timers for fetch-one calls (300ms)
  const fetchTimersRef = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  // Progress debounce timer (1s, global — batches all progress patches)
  const progressTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const pendingProgressRef = useRef(new Map<string, { percent: number; step: string }>());

  // Cleanup timers on unmount
  useEffect(() => {
    return () => {
      fetchTimersRef.current.forEach((t) => clearTimeout(t));
      clearTimeout(progressTimerRef.current);
    };
  }, []);

  // Upsert a fetched task into local state (filter-aware)
  const upsertTask = useCallback((task: TeamTaskData) => {
    const { statusFilter: sf, selectedScope: ss } = filtersRef.current;
    const matches = taskMatchesFilter(task, sf, ss);
    setTasks((prev) => {
      const idx = prev.findIndex((t) => t.id === task.id);
      if (matches) {
        if (idx >= 0) {
          const next = [...prev];
          next[idx] = task;
          return next;
        }
        return [task, ...prev]; // new task — prepend
      }
      // Doesn't match filter → remove if present
      if (idx >= 0) return prev.filter((t) => t.id !== task.id);
      return prev;
    });
  }, []);

  // Debounced fetch-one: fetches a single task and upserts it
  const debouncedFetchTask = useCallback((taskId: string) => {
    const timers = fetchTimersRef.current;
    const existing = timers.get(taskId);
    if (existing) clearTimeout(existing);
    pendingProgressRef.current.delete(taskId); // prevent stale progress overwriting fresh fetch
    timers.set(taskId, setTimeout(async () => {
      timers.delete(taskId);
      try {
        const task = await getTaskLightRef.current(teamId, taskId);
        upsertTask(task);
      } catch {
        // Task may have been deleted between event and fetch — ignore
      }
    }, 300));
  }, [teamId, upsertTask]);

  // Handler: progress events → local patch, 1s debounce
  const onProgress = useCallback((payload: unknown) => {
    const p = payload as TaskEventPayload;
    if (!p?.task_id || (p.team_id && p.team_id !== teamId)) return;
    pendingProgressRef.current.set(p.task_id, {
      percent: p.progress_percent ?? 0,
      step: p.progress_step ?? "",
    });
    clearTimeout(progressTimerRef.current);
    progressTimerRef.current = setTimeout(() => {
      const patches = new Map(pendingProgressRef.current);
      pendingProgressRef.current.clear();
      setTasks((prev) => prev.map((t) => {
        const patch = patches.get(t.id);
        if (!patch) return t;
        return { ...t, progress_percent: patch.percent, progress_step: patch.step };
      }));
    }, 1000);
  }, [teamId]);

  // Handler: deleted → local remove
  const onDeleted = useCallback((payload: unknown) => {
    const p = payload as TaskEventPayload;
    if (!p?.task_id || (p.team_id && p.team_id !== teamId)) return;
    setTasks((prev) => prev.filter((t) => t.id !== p.task_id));
  }, [teamId]);

  // Handler: created / status changes → debounced fetch-one
  const onFetchOne = useCallback((payload: unknown) => {
    const p = payload as TaskEventPayload;
    if (!p?.task_id || (p.team_id && p.team_id !== teamId)) return;
    debouncedFetchTask(p.task_id);
  }, [teamId, debouncedFetchTask]);

  // Subscribe to events with appropriate handlers
  useWsEvent(Events.TEAM_TASK_PROGRESS, onProgress);
  useWsEvent(Events.TEAM_TASK_DELETED, onDeleted);
  useWsEvent(Events.TEAM_TASK_CREATED, onFetchOne);
  useWsEvent(Events.TEAM_TASK_CLAIMED, onFetchOne);
  useWsEvent(Events.TEAM_TASK_COMPLETED, onFetchOne);
  useWsEvent(Events.TEAM_TASK_CANCELLED, onFetchOne);
  useWsEvent(Events.TEAM_TASK_REVIEWED, onFetchOne);
  useWsEvent(Events.TEAM_TASK_APPROVED, onFetchOne);
  useWsEvent(Events.TEAM_TASK_REJECTED, onFetchOne);
  useWsEvent(Events.TEAM_TASK_ASSIGNED, onFetchOne);
  useWsEvent(Events.TEAM_TASK_DISPATCHED, onFetchOne);
  useWsEvent(Events.TEAM_TASK_COMMENTED, onFetchOne); // refresh comment_count badge

  // ── Callbacks for children ──

  const handleRefresh = useCallback(() => load(true), [load]);
  const handleCreateTask = useCallback(() => toast.info(t("board.createViaChat")), [t]);
  const handleTaskClick = useCallback((task: TeamTaskData) => setSelectedTask(task), []);
  const handleCloseDetail = useCallback(() => setSelectedTask(null), []);
  const handleNavigateTask = useCallback((taskId: string) => {
    const found = tasks.find((t) => t.id === taskId);
    if (found) setSelectedTask(found);
  }, [tasks]);

  const deleteTaskRef = useRef(deleteTask);
  deleteTaskRef.current = deleteTask;
  const handleDeleteTask = useCallback((taskId: string) => {
    if (!deleteTaskRef.current) return;
    setDeleteTargetId(taskId);
  }, []);

  const confirmDeleteTask = useCallback(async () => {
    if (!deleteTaskRef.current || !deleteTargetId) return;
    setSingleDeleting(true);
    try {
      await deleteTaskRef.current(teamId, deleteTargetId);
      setDeleteTargetId(null);
    } catch {
      // toast handled by hook
    } finally {
      setSingleDeleting(false);
    }
  }, [teamId, deleteTargetId, t]);

  return (
    <div className="flex flex-1 flex-col gap-3 overflow-hidden p-3 sm:p-4">
      <BoardToolbar
        statusFilter={statusFilter}
        onStatusFilter={setStatusFilter}
        scopes={scopes}
        selectedScope={selectedScope}
        onScopeChange={setSelectedScope}
        spinning={refreshing}
        onRefresh={handleRefresh}
        onCreateTask={handleCreateTask}
        onWorkspace={onWorkspace}
      />

      <div className="flex flex-1 flex-col min-h-0 overflow-hidden">
        {!initialized ? (
          <div className="py-12 text-center text-sm text-muted-foreground">{t("tasks.loading")}</div>
        ) : viewMode === "kanban" ? (
          <KanbanBoard
            tasks={tasks}
            isTeamV2={isTeamV2}
            groupBy={groupBy}
            emojiLookup={emojiLookup}
            memberLookup={memberLookup}
            taskLookup={taskLookup}
            onTaskClick={handleTaskClick}
            onDeleteTask={deleteTask ? handleDeleteTask : undefined}
          />
        ) : (
          <TaskList
            tasks={tasks}
            loading={!initialized}
            teamId={teamId}
            members={members}
            isTeamV2={isTeamV2}
            emojiLookup={emojiLookup}
            getTaskDetail={getTaskDetail}
            deleteTask={deleteTask}
            deleteTasksBulk={deleteTasksBulk}
            addTaskComment={addTaskComment}
          />
        )}
      </div>

      <ConfirmDialog
        open={!!deleteTargetId}
        onOpenChange={(v) => !v && setDeleteTargetId(null)}
        title={t("tasks.delete")}
        description={t("tasks.deleteConfirm")}
        confirmLabel={t("tasks.delete")}
        variant="destructive"
        onConfirm={confirmDeleteTask}
        loading={singleDeleting}
      />

      {selectedTask && (
        <TaskDetailDialog
          task={selectedTask}
          teamId={teamId}
          isTeamV2={isTeamV2}
          onClose={handleCloseDetail}
          getTaskDetail={getTaskDetail}
          deleteTask={deleteTask}
          taskLookup={taskLookup}
          memberLookup={memberLookup}
          emojiLookup={emojiLookup}
          onNavigateTask={handleNavigateTask}
        />
      )}
    </div>
  );
});
