import { useState, useMemo } from "react";
import { ClipboardList, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";
import type { TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment } from "@/types/team";
import type { TeamMemberData } from "@/types/team";
import { taskStatusBadgeVariant, isTerminalStatus } from "./task-utils";
import { TaskDetailDialog } from "./task-detail-dialog";
import { buildTaskLookup, buildMemberLookup } from "../board/board-utils";

interface TaskListProps {
  tasks: TeamTaskData[];
  loading: boolean;
  teamId: string;
  members: TeamMemberData[];
  isTeamV2?: boolean;
  emojiLookup?: Map<string, string>;
  getTaskDetail: (teamId: string, taskId: string) => Promise<{
    task: TeamTaskData; comments: TeamTaskComment[];
    events: TeamTaskEvent[]; attachments: TeamTaskAttachment[];
  }>;
  deleteTask?: (teamId: string, taskId: string) => Promise<void>;
}

export function TaskList({
  tasks, loading, teamId, members, isTeamV2, emojiLookup,
  getTaskDetail, deleteTask,
}: TaskListProps) {
  const { t } = useTranslation("teams");
  const [selectedTask, setSelectedTask] = useState<TeamTaskData | null>(null);
  const taskLookup = useMemo(() => buildTaskLookup(tasks), [tasks]);
  const memberLookup = useMemo(() => buildMemberLookup(members), [members]);

  if (loading && tasks.length === 0) {
    return <div className="py-8 text-center text-sm text-muted-foreground">{t("tasks.loading")}</div>;
  }

  if (tasks.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-8 text-center">
        <ClipboardList className="h-8 w-8 text-muted-foreground/50" />
        <p className="text-sm text-muted-foreground">{t("tasks.noTasks")}</p>
        <p className="text-xs text-muted-foreground">{t("tasks.noTasksDescription")}</p>
      </div>
    );
  }

  const handleDelete = (e: React.MouseEvent, taskId: string) => {
    e.stopPropagation();
    if (!deleteTask) return;
    if (!window.confirm(t("tasks.deleteConfirm"))) return;
    deleteTask(teamId, taskId);
  };

  return (
    <>
      <div className="overflow-x-auto rounded-lg border">
        <div className="grid min-w-[500px] grid-cols-[70px_1fr_90px_100px_60px_40px] items-center gap-2 border-b bg-muted/50 px-4 py-2.5 text-xs font-medium text-muted-foreground">
          <span>{t("tasks.columns.id")}</span>
          <span>{t("tasks.columns.subject")}</span>
          <span>{t("tasks.columns.status")}</span>
          <span>{t("tasks.columns.owner")}</span>
          <span>{t("tasks.columns.priority")}</span>
          <span />
        </div>
        {tasks.map((task) => {
          const ownerEmoji = task.owner_agent_id && emojiLookup?.get(task.owner_agent_id);
          return (
            <div
              key={task.id}
              className="grid min-w-[500px] cursor-pointer grid-cols-[70px_1fr_90px_100px_60px_40px] items-center gap-2 border-b px-4 py-3 last:border-0 hover:bg-muted/30"
              onClick={() => setSelectedTask(task)}
            >
              <span className="font-mono text-xs text-muted-foreground">{task.identifier || "\u2014"}</span>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{task.subject}</p>
                {task.description && (
                  <p className="truncate text-xs text-muted-foreground/70">{task.description}</p>
                )}
                {task.task_type && task.task_type !== "general" && (
                  <Badge variant="outline" className="mt-0.5 text-[10px]">{task.task_type}</Badge>
                )}
              </div>
              <div className="flex flex-wrap items-center gap-1">
                <Badge variant={taskStatusBadgeVariant(task.status)}>{task.status.replace(/_/g, " ")}</Badge>
                {isTeamV2 && task.followup_at && task.status === "in_progress" && (
                  <Badge variant="outline" className="border-amber-500/50 bg-amber-500/10 text-[10px] text-amber-700 dark:text-amber-400">
                    {t("tasks.badges.awaitingReply")}
                  </Badge>
                )}
              </div>
              <span className="flex items-center gap-1 truncate text-sm text-muted-foreground">
                {ownerEmoji && <span className="text-sm leading-none">{ownerEmoji}</span>}
                {task.owner_agent_key || "\u2014"}
              </span>
              <span className="text-sm text-muted-foreground">{task.priority}</span>
              <div>
                {deleteTask && isTerminalStatus(task.status) && (
                  <button
                    className="rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                    onClick={(e) => handleDelete(e, task.id)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {selectedTask && (
        <TaskDetailDialog
          task={selectedTask}
          teamId={teamId}
          isTeamV2={isTeamV2}
          onClose={() => setSelectedTask(null)}
          getTaskDetail={getTaskDetail}
          deleteTask={deleteTask}
          taskLookup={taskLookup}
          memberLookup={memberLookup}
          emojiLookup={emojiLookup}
          onNavigateTask={(taskId) => {
            const found = tasks.find((t) => t.id === taskId);
            if (found) setSelectedTask(found);
          }}
        />
      )}
    </>
  );
}
