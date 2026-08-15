import { cn } from "@/lib/utils";

type Status = "success" | "warning" | "error" | "info" | "default";

const statusClasses: Record<Status, string> = {
  success: "bg-green-500/15 text-green-600 dark:text-green-400",
  warning: "bg-yellow-500/15 text-yellow-600 dark:text-yellow-400",
  error: "bg-red-500/15 text-red-600 dark:text-red-400",
  info: "bg-blue-500/15 text-blue-600 dark:text-blue-400",
  default: "bg-muted text-muted-foreground",
};

interface StatusBadgeProps {
  status: Status;
  label: string;
  className?: string;
}

export function StatusBadge({ status, label, className }: StatusBadgeProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium",
        statusClasses[status],
        className,
      )}
    >
      <span className={cn(
        "h-1.5 w-1.5 rounded-full",
        status === "success" && "bg-green-500",
        status === "warning" && "bg-yellow-500",
        status === "error" && "bg-red-500",
        status === "info" && "bg-blue-500",
        status === "default" && "bg-muted-foreground",
      )} />
      {label}
    </span>
  );
}
