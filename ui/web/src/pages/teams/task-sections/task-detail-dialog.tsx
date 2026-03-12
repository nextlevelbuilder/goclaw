import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";
import { formatDate } from "@/lib/format";
import type { TeamTaskData } from "@/types/team";
import { taskStatusBadgeVariant } from "./task-utils";

interface TaskDetailDialogProps {
  task: TeamTaskData;
  onClose: () => void;
}

export function TaskDetailDialog({ task, onClose }: TaskDetailDialogProps) {
  const { t } = useTranslation("teams");

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] w-[95vw] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{t("tasks.detail.title")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* Subject */}
          <div className="rounded-md border p-3">
            <p className="mb-1 text-xs font-medium text-muted-foreground">{t("tasks.detail.subject")}</p>
            <p className="text-sm font-medium">{task.subject}</p>
          </div>

          {/* Summary grid */}
          <div className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
            <div>
              <span className="text-muted-foreground">{t("tasks.detail.status")}</span>{" "}
              <Badge variant={taskStatusBadgeVariant(task.status)} className="text-xs">
                {task.status.replace("_", " ")}
              </Badge>
            </div>
            <div>
              <span className="text-muted-foreground">{t("tasks.detail.priority")}</span>{" "}
              <span className="font-medium">{task.priority}</span>
            </div>
            <div>
              <span className="text-muted-foreground">{t("tasks.detail.owner")}</span>{" "}
              <span className="font-medium">{task.owner_agent_key || "—"}</span>
            </div>
            {task.created_at && (
              <div>
                <span className="text-muted-foreground">{t("tasks.detail.created")}</span>{" "}
                {formatDate(task.created_at)}
              </div>
            )}
            {task.updated_at && (
              <div>
                <span className="text-muted-foreground">{t("tasks.detail.updated")}</span>{" "}
                {formatDate(task.updated_at)}
              </div>
            )}
          </div>

          {/* Blocked by */}
          {task.blocked_by && task.blocked_by.length > 0 && (
            <div className="text-sm">
              <span className="text-muted-foreground">{t("tasks.detail.blockedBy")}</span>{" "}
              <span className="font-mono text-xs">{task.blocked_by.join(", ")}</span>
            </div>
          )}

          {/* Description */}
          {task.description && (
            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs font-medium text-muted-foreground">{t("tasks.detail.description")}</p>
              <pre className="whitespace-pre-wrap break-words text-sm">{task.description}</pre>
            </div>
          )}

          {/* Result */}
          {task.result && (
            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs font-medium text-muted-foreground">{t("tasks.detail.result")}</p>
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
