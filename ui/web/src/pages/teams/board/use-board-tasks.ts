import { useState, useCallback, useEffect, useRef } from "react";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Events } from "@/api/protocol";
import type { TeamTaskData, ScopeEntry } from "@/types/team";

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

/** Check if a task matches the current filters. */
function taskMatchesFilter(task: TeamTaskData, sf: StatusFilter, scope: ScopeEntry | null): boolean {
  switch (sf) {
    case "pending": if (task.status !== "pending") return false; break;
    case "in_progress": if (task.status !== "in_progress") return false; break;
    case "completed": if (task.status !== "completed" && task.status !== "cancelled") return false; break;
  }
  if (scope) {
    if (scope.channel && (task.channel ?? "") !== scope.channel) return false;
    if (scope.chat_id && (task.chat_id ?? "") !== scope.chat_id) return false;
  }
  return true;
}

interface UseBoardTasksParams {
  teamId: string;
  getTeamTasks: (teamId: string, status?: string, channel?: string, chatId?: string) => Promise<{ tasks: TeamTaskData[]; count: number }>;
  getTaskLight: (teamId: string, taskId: string) => Promise<TeamTaskData>;
  statusFilter: StatusFilter;
  selectedScope: ScopeEntry | null;
}

interface UseBoardTasksResult {
  tasks: TeamTaskData[];
  initialized: boolean;
  refreshing: boolean;
  load: (showSpinner?: boolean) => Promise<void>;
}

export function useBoardTasks({
  teamId,
  getTeamTasks,
  getTaskLight,
  statusFilter,
  selectedScope,
}: UseBoardTasksParams): UseBoardTasksResult {
  const [tasks, setTasks] = useState<TeamTaskData[]>([]);
  const [initialized, setInitialized] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  // Stable refs for filter values — avoids recreating load callback
  const filtersRef = useRef({ statusFilter, selectedScope });
  filtersRef.current = { statusFilter, selectedScope };
  const getTeamTasksRef = useRef(getTeamTasks);
  getTeamTasksRef.current = getTeamTasks;
  const getTaskLightRef = useRef(getTaskLight);
  getTaskLightRef.current = getTaskLight;

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
      console.error("[useBoardTasks] load failed:", err);
    } finally {
      if (showSpinner) setRefreshing(false);
    }
  }, [teamId]);

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
        return [task, ...prev];
      }
      if (idx >= 0) return prev.filter((t) => t.id !== task.id);
      return prev;
    });
  }, []);

  // Debounced fetch-one: fetches a single task and upserts it
  const debouncedFetchTask = useCallback((taskId: string) => {
    const timers = fetchTimersRef.current;
    const existing = timers.get(taskId);
    if (existing) clearTimeout(existing);
    pendingProgressRef.current.delete(taskId);
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
  useWsEvent(Events.TEAM_TASK_COMMENTED, onFetchOne);

  return { tasks, initialized, refreshing, load };
}
