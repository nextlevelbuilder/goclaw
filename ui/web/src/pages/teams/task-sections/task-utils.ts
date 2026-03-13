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
