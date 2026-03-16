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
import type {
  TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment,
  TeamMemberData, ScopeEntry,
} from "@/types/team";

type StatusFilter = "all" | "pending" | "in_progress" | "completed";

interface BoardContainerProps {
  teamId: string;
  members: TeamMemberData[];
  scopes: ScopeEntry[];
  isTeamV2: boolean;
  getTeamTasks: (teamId: string, status?: string, channel?: string, chatId?: string) => Promise<{ tasks: TeamTaskData[]; count: number }>;
  getTaskDetail: (teamId: string, taskId: string) => Promise<{ task: TeamTaskData; comments: TeamTaskComment[]; events: TeamTaskEvent[]; attachments: TeamTaskAttachment[] }>;
  deleteTask?: (teamId: string, taskId: string) => Promise<void>;
  onWorkspace?: () => void;
}

export const BoardContainer = memo(function BoardContainer({
  teamId, members, scopes, isTeamV2,
  getTeamTasks, getTaskDetail, deleteTask, onWorkspace,
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

  // Lookups for name resolution
  const taskLookup = useMemo(() => buildTaskLookup(tasks), [tasks]);
  const memberLookup = useMemo(() => buildMemberLookup(members), [members]);
  const emojiLookup = useMemo(() => buildEmojiLookup(members), [members]);

  // Stable refs for filter values — avoids recreating load callback
  const filtersRef = useRef({ statusFilter, selectedScope });
  filtersRef.current = { statusFilter, selectedScope };
  const getTeamTasksRef = useRef(getTeamTasks);
  getTeamTasksRef.current = getTeamTasks;

  // Stable load function — never changes identity
  const load = useCallback(async (showSpinner = false) => {
    if (showSpinner) setRefreshing(true);
    try {
      const { statusFilter: sf, selectedScope: ss } = filtersRef.current;
      // Backend supports: "" (active), "all", "completed", "in_review"
      // "pending" and "in_progress" are client-side filtered from "all"
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

  // Initial load
  useEffect(() => { load(); }, [load]);

  // Re-fetch when filters change — seamless (no spinner)
  useEffect(() => {
    if (initialized) load();
  }, [statusFilter, selectedScope]); // eslint-disable-line react-hooks/exhaustive-deps

  // Debounced WS event reload
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const reloadOnEvent = useCallback((payload: unknown) => {
    const p = payload as { team_id?: string };
    if (p?.team_id && p.team_id !== teamId) return;
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => load(), 300);
  }, [teamId, load]);

  useWsEvent(Events.TEAM_TASK_CREATED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_CLAIMED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_COMPLETED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_CANCELLED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_REVIEWED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_APPROVED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_REJECTED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_PROGRESS, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_COMMENTED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_ASSIGNED, reloadOnEvent);
  useWsEvent(Events.TEAM_TASK_DELETED, reloadOnEvent);

  // Stable callbacks for children — never change identity
  const handleRefresh = useCallback(() => load(true), [load]);
  const handleCreateTask = useCallback(() => toast.info(t("board.createViaChat")), [t]);
  const handleTaskClick = useCallback((task: TeamTaskData) => setSelectedTask(task), []);
  const handleCloseDetail = useCallback(() => setSelectedTask(null), []);

  // Delete handler for kanban cards (confirm + call API)
  const deleteTaskRef = useRef(deleteTask);
  deleteTaskRef.current = deleteTask;
  const handleDeleteTask = useCallback(async (taskId: string) => {
    if (!deleteTaskRef.current) return;
    if (!window.confirm(t("tasks.deleteConfirm"))) return;
    try {
      await deleteTaskRef.current(teamId, taskId);
      toast.success(t("toast.taskDeleted"));
    } catch {
      toast.error(t("toast.failedDeleteTask"));
    }
  }, [teamId, t]);

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
          />
        )}
      </div>

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
        />
      )}
    </div>
  );
});
