import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { formatDate } from "@/lib/format";
import type { TeamTaskData } from "@/types/team";
import { taskStatusBadgeVariant } from "./task-utils";

interface TaskDetailDialogProps {
  task: TeamTaskData;
  onClose: () => void;
}

export function TaskDetailDialog({ task, onClose }: TaskDetailDialogProps) {
  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] w-[95vw] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>Task Detail</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* Subject */}
          <div className="rounded-md border p-3">
            <p className="mb-1 text-xs font-medium text-muted-foreground">Subject</p>
            <p className="text-sm font-medium">{task.subject}</p>
          </div>

          {/* Summary grid */}
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div>
              <span className="text-muted-foreground">Status:</span>{" "}
              <Badge variant={taskStatusBadgeVariant(task.status)} className="text-xs">
                {task.status.replace("_", " ")}
              </Badge>
            </div>
            <div>
              <span className="text-muted-foreground">Priority:</span>{" "}
              <span className="font-medium">{task.priority}</span>
            </div>
            <div>
              <span className="text-muted-foreground">Owner:</span>{" "}
              <span className="font-medium">{task.owner_agent_key || "—"}</span>
            </div>
            {task.created_at && (
              <div>
                <span className="text-muted-foreground">Created:</span>{" "}
                {formatDate(task.created_at)}
              </div>
            )}
            {task.updated_at && (
              <div>
                <span className="text-muted-foreground">Updated:</span>{" "}
                {formatDate(task.updated_at)}
              </div>
            )}
          </div>

          {/* Blocked by */}
          {task.blocked_by && task.blocked_by.length > 0 && (
            <div className="text-sm">
              <span className="text-muted-foreground">Blocked by:</span>{" "}
              <span className="font-mono text-xs">{task.blocked_by.join(", ")}</span>
            </div>
          )}

          {/* Description */}
          {task.description && (
            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs font-medium text-muted-foreground">Description</p>
              <pre className="whitespace-pre-wrap break-words text-sm">{task.description}</pre>
            </div>
          )}

          {/* Result */}
          {task.result && (
            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs font-medium text-muted-foreground">Result</p>
              <pre className="max-h-[40vh] overflow-y-auto whitespace-pre-wrap break-words text-sm">
                {task.result}
              </pre>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
