import { Badge } from "@/components/ui/badge";
import type { TeamEventEntry } from "@/stores/use-team-event-store";
import { taskStatusBadgeVariant } from "@/pages/teams/task-sections";
import type { TeamTaskEventPayload } from "@/types/team-events";

interface Props {
  entry: TeamEventEntry;
  resolveAgent: (keyOrId: string | undefined) => string;
}

export function TaskEventCard({ entry, resolveAgent }: Props) {
  const p = entry.payload as TeamTaskEventPayload;
  const owner = p.owner_display_name || resolveAgent(p.owner_agent_key);
  return (
    <div className="space-y-1 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5">
        {p.subject && <span className="min-w-0 truncate font-medium">{p.subject}</span>}
        <Badge variant={taskStatusBadgeVariant(p.status)} className="shrink-0 text-xs">
          {p.status}
        </Badge>
      </div>
      <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
        {p.task_id && (
          <span className="rounded bg-muted px-1.5 py-0.5 font-mono">
            task: {p.task_id.slice(0, 8)}
          </span>
        )}
        {p.owner_agent_key && (
          <span className="rounded bg-muted px-1.5 py-0.5 truncate">Owner: {owner}</span>
        )}
        {p.reason && <span className="break-words">Reason: {p.reason}</span>}
      </div>
    </div>
  );
}
