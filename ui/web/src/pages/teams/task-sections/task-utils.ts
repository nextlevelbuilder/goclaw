export function taskStatusBadgeVariant(status: string) {
  switch (status) {
    case "pending": return "outline" as const;
    case "pending_approval": return "default" as const;
    case "in_progress": return "info" as const;
    case "completed": return "success" as const;
    case "blocked": return "warning" as const;
    case "failed": return "destructive" as const;
    case "in_review": return "secondary" as const;
    case "cancelled": return "outline" as const;
    default: return "outline" as const;
  }
}

/** Whether the task can be acted on (approve/reject/cancel) */
export function isTaskActionable(status: string) {
  return status !== "completed" && status !== "failed";
}

const TERMINAL_STATUSES = new Set(["completed", "failed", "cancelled"]);

/** Whether the task is in a terminal status and can be deleted */
export function isTerminalStatus(status: string) {
  return TERMINAL_STATUSES.has(status);
}

/** Whether a human can cancel the task (teams.tasks.cancel): anything not completed/cancelled */
export function canCancelTask(status: string) {
  return status !== "completed" && status !== "cancelled";
}

const RETRYABLE_STATUSES = new Set(["stale", "failed", "cancelled", "in_review", "blocked"]);

/**
 * Whether a human can restart the task (teams.tasks.retry). Mirrors the
 * backend: blocked tasks qualify only once nothing blocks them any more.
 */
export function canRetryTask(task: { status: string; blocked_by?: string[] | null }) {
  if (!RETRYABLE_STATUSES.has(task.status)) return false;
  if (task.status === "blocked" && task.blocked_by && task.blocked_by.length > 0) return false;
  return true;
}
