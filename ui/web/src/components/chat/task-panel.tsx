import { X, MessageSquare, Paperclip } from "lucide-react";
import { cn } from "@/lib/utils";
import type { ActiveTeamTask } from "@/types/chat";

interface TaskPanelProps {
  tasks: ActiveTeamTask[];
  open: boolean;
  onClose: () => void;
}

export function TaskPanel({ tasks, open, onClose }: TaskPanelProps) {
  if (!open) return null;

  return (
    <div className={cn(
      "flex h-full w-72 shrink-0 flex-col border-l bg-background",
      "max-sm:fixed max-sm:inset-y-0 max-sm:right-0 max-sm:z-50 max-sm:w-full max-sm:max-w-[85vw] max-sm:shadow-xl",
    )}>
      {/* Header */}
      <div className="flex items-center justify-between border-b px-3 py-2">
        <span className="text-sm font-medium">
          Running Tasks ({tasks.length})
        </span>
        <button
          type="button"
          onClick={onClose}
          className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Task list */}
      <div className="flex-1 overflow-y-auto overscroll-contain p-2 space-y-2">
        {tasks.length === 0 ? (
          <p className="px-2 py-4 text-center text-xs text-muted-foreground">No active tasks</p>
        ) : (
          tasks.map((task) => <TaskCard key={task.taskId} task={task} />)
        )}
      </div>
    </div>
  );
}

function TaskCard({ task }: { task: ActiveTeamTask }) {
  const pct = task.progressPercent;

  return (
    <div className="rounded-lg border bg-card p-2.5 text-xs shadow-sm">
      {/* Header: number + owner + counters */}
      <div className="flex items-center gap-1.5 text-muted-foreground">
        <span className="font-mono">#{task.taskNumber}</span>
        <span className="truncate">{task.ownerDisplayName || task.ownerAgentKey || "unassigned"}</span>
        <span className="ml-auto flex items-center gap-2">
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
        </span>
      </div>

      {/* Progress bar — above the step message */}
      {pct != null && (
        <div className="mt-1.5">
          <div className="flex items-center gap-1.5">
            <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-primary transition-all duration-300"
                style={{ width: `${Math.min(pct, 100)}%` }}
              />
            </div>
            <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">{pct}%</span>
          </div>
        </div>
      )}

      {/* Step message — full text, no truncation */}
      {task.progressStep && (
        <p className="mt-1 text-[11px] leading-snug text-muted-foreground">
          {task.progressStep}
        </p>
      )}

      {/* Subject — full text */}
      <p className="mt-1.5 font-medium leading-snug">{task.subject}</p>
    </div>
  );
}
