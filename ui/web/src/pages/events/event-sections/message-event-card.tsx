import { Badge } from "@/components/ui/badge";
import type { TeamEventEntry } from "@/stores/use-team-event-store";
import type { TeamMessageEventPayload } from "@/types/team-events";

interface Props {
  entry: TeamEventEntry;
  resolveAgent: (keyOrId: string | undefined) => string;
}

export function MessageEventCard({ entry, resolveAgent }: Props) {
  const p = entry.payload as TeamMessageEventPayload;
  const from = p.from_display_name || resolveAgent(p.from_agent_key);
  const to =
    p.to_agent_key === "broadcast"
      ? "all"
      : p.to_display_name || resolveAgent(p.to_agent_key);
  return (
    <div className="space-y-1 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5">
        <span className="truncate font-medium">{from}</span>
        <span className="shrink-0 text-muted-foreground">&rarr;</span>
        <span className="truncate font-medium">{to}</span>
        <Badge variant="outline" className="shrink-0 text-xs">
          {p.message_type}
        </Badge>
      </div>
      {p.preview && (
        <p className="break-words text-xs text-muted-foreground line-clamp-2">{p.preview}</p>
      )}
      {p.task_id && (
        <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
          task: {p.task_id.slice(0, 8)}
        </span>
      )}
    </div>
  );
}
