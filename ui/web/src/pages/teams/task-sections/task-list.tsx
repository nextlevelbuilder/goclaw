import { useState } from "react";
import { ClipboardList } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { TeamTaskData } from "@/types/team";
import { taskStatusBadgeVariant } from "./task-utils";
import { TaskDetailDialog } from "./task-detail-dialog";

interface TaskListProps {
  tasks: TeamTaskData[];
  loading: boolean;
}

export function TaskList({ tasks, loading }: TaskListProps) {
  const [selectedTask, setSelectedTask] = useState<TeamTaskData | null>(null);

  if (loading && tasks.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-muted-foreground">
        Loading tasks...
      </div>
    );
  }

  if (tasks.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-8 text-center">
        <ClipboardList className="h-8 w-8 text-muted-foreground/50" />
        <p className="text-sm text-muted-foreground">No tasks yet</p>
        <p className="text-xs text-muted-foreground">
          Tasks are created by team agents during collaboration.
        </p>
      </div>
    );
  }

  return (
    <>
      <div className="overflow-x-auto rounded-lg border">
        <div className="grid min-w-[400px] grid-cols-[1fr_90px_100px_60px] items-center gap-2 border-b bg-muted/50 px-4 py-2.5 text-xs font-medium text-muted-foreground">
          <span>Subject</span>
          <span>Status</span>
          <span>Owner</span>
          <span>Priority</span>
        </div>
        {tasks.map((task) => (
          <div
            key={task.id}
            className="grid min-w-[400px] cursor-pointer grid-cols-[1fr_90px_100px_60px] items-center gap-2 border-b px-4 py-3 last:border-0 hover:bg-muted/30"
            onClick={() => setSelectedTask(task)}
          >
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{task.subject}</p>
              {task.description && (
                <p className="truncate text-xs text-muted-foreground/70">
                  {task.description}
                </p>
              )}
              {task.result && (
                <p className="mt-0.5 line-clamp-1 text-xs text-emerald-600 dark:text-emerald-400">
                  {task.result}
                </p>
              )}
            </div>
            <Badge variant={taskStatusBadgeVariant(task.status)}>
              {task.status.replace("_", " ")}
            </Badge>
            <span className="truncate text-sm text-muted-foreground">
              {task.owner_agent_key || "—"}
            </span>
            <span className="text-sm text-muted-foreground">
              {task.priority}
            </span>
          </div>
        ))}
      </div>

      {selectedTask && (
        <TaskDetailDialog
          task={selectedTask}
          onClose={() => setSelectedTask(null)}
        />
      )}
    </>
  );
}
