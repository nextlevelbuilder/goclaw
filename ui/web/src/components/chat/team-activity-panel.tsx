import { Users, MessageSquare, Paperclip } from "lucide-react";
import type { ActiveTeamTask } from "@/types/chat";

interface TeamActivityPanelProps {
  tasks: ActiveTeamTask[];
  onTogglePanel?: () => void;
}

export function TeamActivityPanel({ tasks, onTogglePanel }: TeamActivityPanelProps) {
  if (tasks.length === 0) return null;

  return (
    <div className="rounded-lg border bg-muted px-3 py-2">
      <button
        type="button"
        className="mb-1.5 flex w-full items-center gap-2 text-xs font-medium text-muted-foreground hover:text-foreground"
        onClick={onTogglePanel}
      >
        <Users className="h-3.5 w-3.5" />
        <span>
          Team: {tasks.length} task{tasks.length > 1 ? "s" : ""} active
        </span>
      </button>
      <div className="space-y-1">
        {tasks.map((task) => (
          <div key={task.taskId} className="flex items-center gap-2 text-xs">
            <span className="text-muted-foreground">#{task.taskNumber}</span>
            <span className="truncate font-medium">{task.subject}</span>
            <span className="text-muted-foreground">&rarr;</span>
            <span className="shrink-0 text-muted-foreground">
              {task.ownerDisplayName || task.ownerAgentKey}
            </span>
            <span className="ml-auto flex shrink-0 items-center gap-2 text-muted-foreground">
              {(task.commentCount ?? 0) > 0 && (
                <span className="flex items-center gap-0.5">
                  <MessageSquare className="h-3 w-3" /> {task.commentCount}
                </span>
              )}
              {(task.attachmentCount ?? 0) > 0 && (
                <span className="flex items-center gap-0.5">
                  <Paperclip className="h-3 w-3" /> {task.attachmentCount}
                </span>
              )}
              {task.progressPercent != null && (
                <span>{task.progressPercent}%</span>
              )}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
