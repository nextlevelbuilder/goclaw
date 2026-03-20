import { Users } from "lucide-react";
import type { ActiveTeamTask } from "@/types/chat";

interface TeamActivityPanelProps {
  tasks: ActiveTeamTask[];
}

export function TeamActivityPanel({ tasks }: TeamActivityPanelProps) {
  if (tasks.length === 0) return null;

  return (
    <div className="rounded-lg border bg-muted px-3 py-2">
      <div className="mb-1.5 flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <Users className="h-3.5 w-3.5" />
        <span>
          Team: {tasks.length} task{tasks.length > 1 ? "s" : ""} active
        </span>
      </div>
      <div className="space-y-1">
        {tasks.map((task) => (
          <div key={task.taskId} className="flex items-center gap-2 text-xs">
            <span className="text-muted-foreground">#{task.taskNumber}</span>
            <span className="truncate font-medium">{task.subject}</span>
            <span className="text-muted-foreground">→</span>
            <span className="shrink-0 text-muted-foreground">
              {task.ownerDisplayName || task.ownerAgentKey}
            </span>
            {task.progressPercent != null && (
              <span className="ml-auto shrink-0 text-muted-foreground">
                {task.progressPercent}%
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
