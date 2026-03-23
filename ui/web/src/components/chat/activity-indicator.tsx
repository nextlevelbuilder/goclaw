import { Brain, Wrench, Pencil, Archive, RefreshCw, Users } from "lucide-react";
import type { RunActivity } from "@/types/chat";

interface ActivityIndicatorProps {
  activity: RunActivity | null;
  isRunning: boolean;
}

export function ActivityIndicator({ activity, isRunning }: ActivityIndicatorProps) {
  if (!isRunning && activity?.phase !== "leader_processing") return null;

  if (!activity) {
    return (
      <div className="flex items-center gap-1 px-1 py-1">
        <span className="flex gap-1">
          <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:0ms]" />
          <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:150ms]" />
          <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:300ms]" />
        </span>
      </div>
    );
  }

  const config = getPhaseConfig(activity);

  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <config.icon className={`h-4 w-4 animate-pulse ${config.color}`} />
      <span className={config.color}>{config.label}</span>
    </div>
  );
}

function getPhaseConfig(activity: RunActivity) {
  switch (activity.phase) {
    case "thinking":
      return { icon: Brain, color: "text-orange-500", label: "Thinking..." };
    case "tool_exec":
      return {
        icon: Wrench,
        color: "text-blue-500",
        label: activity.tool ? `Running ${activity.tool}...` : "Running tools...",
      };
    case "streaming":
      return { icon: Pencil, color: "text-foreground", label: "Writing..." };
    case "compacting":
      return { icon: Archive, color: "text-amber-500", label: "Optimizing context..." };
    case "retrying":
      return {
        icon: RefreshCw,
        color: "text-orange-500",
        label: `Retrying (${activity.retryAttempt ?? 0}/${activity.retryMax ?? 0})...`,
      };
    case "leader_processing":
      return { icon: Users, color: "text-emerald-500", label: "Processing team results..." };
    default:
      return { icon: Brain, color: "text-muted-foreground", label: "Working..." };
  }
}
