import { useState, useEffect, useCallback, useMemo, useRef, memo, lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { useBoardStore } from "../stores/use-board-store";
import { toast } from "@/stores/use-toast-store";
import { buildTaskLookup, buildMemberLookup, buildEmojiLookup } from "./board-utils";
import { BoardToolbar } from "./board-toolbar";
import { KanbanBoard } from "./kanban-board";
import { TaskList } from "../task-sections";

const TaskDetailDialog = lazy(() =>
  import("../task-sections/task-detail-dialog").then((m) => ({ default: m.TaskDetailDialog }))
);
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useBoardTasks } from "./use-board-tasks";
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
  getTaskLight: (teamId: string, taskId: string) => Promise<TeamTaskData>;
  deleteTask?: (teamId: string, taskId: string) => Promise<void>;
  deleteTasksBulk?: (teamId: string, taskIds: string[]) => Promise<number>;
  addTaskComment?: (teamId: string, taskId: string, content: string) => Promise<void>;
  onWorkspace?: () => void;
}

export const BoardContainer = memo(function BoardContainer({
  teamId, members, scopes, isTeamV2,
  getTeamTasks, getTaskDetail, getTaskLight, deleteTask, deleteTasksBulk, addTaskComment, onWorkspace,
}: BoardContainerProps) {
  const { t } = useTranslation("teams");
  const viewMode = useBoardStore((s) => s.viewMode);
  const groupBy = useBoardStore((s) => s.groupBy);

  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [selectedScope, setSelectedScope] = useState<ScopeEntry | null>(null);
  const [selectedTask, setSelectedTask] = useState<TeamTaskData | null>(null);
  const [deleteTargetId, setDeleteTargetId] = useState<string | null>(null);
  const [singleDeleting, setSingleDeleting] = useState(false);

  const { tasks, initialized, refreshing, load } = useBoardTasks({
    teamId,
    getTeamTasks,
    getTaskLight,
    statusFilter,
    selectedScope,
  });

  // Lookups for name resolution
  const taskLookup = useMemo(() => buildTaskLookup(tasks), [tasks]);
  const memberLookup = useMemo(() => buildMemberLookup(members), [members]);
  const emojiLookup = useMemo(() => buildEmojiLookup(members), [members]);

  useEffect(() => { load(); }, [load]);

  // Re-fetch when filters change
  useEffect(() => {
    if (initialized) load();
  }, [statusFilter, selectedScope]); // eslint-disable-line react-hooks/exhaustive-deps

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
        <Suspense fallback={null}>
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
        </Suspense>
      )}
    </div>
  );
});
