import { useState } from "react";
import { Radio, User } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { formatRelativeTime } from "@/lib/format";
import type { TeamEventEntry } from "@/stores/use-team-event-store";
import { useAgentResolver } from "./use-agent-resolver";
import { getCategoryConfig } from "./event-categories";
import { TaskEventCard } from "./task-event-cards";
import { MessageEventCard } from "./message-event-card";
import { AgentEventCard } from "./agent-event-cards";
import { TeamCrudEventCard } from "./team-crud-event-cards";
import { EventDetailDialog } from "./event-detail-dialog";

interface EventCardProps {
  entry: TeamEventEntry;
  resolveTeam: (teamId: string | null) => string;
}

export function EventCard({ entry, resolveTeam }: EventCardProps) {
  const { event } = entry;
  const { resolveAgent } = useAgentResolver();
  const [showDetail, setShowDetail] = useState(false);
  const config = getCategoryConfig(event);
  const CategoryIcon = config.icon;

  let content: React.ReactNode;
  if (event.startsWith("team.task.")) {
    content = <TaskEventCard entry={entry} resolveAgent={resolveAgent} />;
  } else if (event === "team.message.sent") {
    content = <MessageEventCard entry={entry} resolveAgent={resolveAgent} />;
  } else if (event === "agent") {
    content = <AgentEventCard entry={entry} resolveAgent={resolveAgent} />;
  } else if (
    event.startsWith("team.created") ||
    event.startsWith("team.updated") ||
    event.startsWith("team.deleted") ||
    event.startsWith("team.member.") ||
    event.startsWith("agent_link.")
  ) {
    content = <TeamCrudEventCard entry={entry} resolveAgent={resolveAgent} />;
  } else {
    content = (
      <pre className="overflow-x-auto text-xs text-muted-foreground">
        {JSON.stringify(entry.payload, null, 2)}
      </pre>
    );
  }

  return (
    <>
      <button
        type="button"
        onClick={() => setShowDetail(true)}
        className={cn(
          "w-full cursor-pointer overflow-hidden rounded-lg border border-l-4 bg-card px-3 py-2.5 text-left transition-colors hover:bg-accent/50",
          config.borderColor,
        )}
      >
        {/* Header: icon + type badge + team + time */}
        <div className="flex items-center gap-2">
          <CategoryIcon className={cn("h-3.5 w-3.5 shrink-0", config.iconColor)} />
          <EventTypeBadge event={event} payload={entry.payload} />
          {entry.teamId && (
            <Badge variant="outline" className="shrink-0 text-xs">
              {resolveTeam(entry.teamId)}
            </Badge>
          )}
          <span className="ml-auto shrink-0 text-xs text-muted-foreground">
            {formatRelativeTime(new Date(entry.timestamp))}
          </span>
        </div>

        {/* Body: content from sub-card */}
        <div className="mt-1.5 min-w-0 pl-[22px]">{content}</div>

        {/* Footer: metadata tags */}
        <EventMetadataFooter payload={entry.payload} />
      </button>

      {showDetail && (
        <EventDetailDialog entry={entry} onClose={() => setShowDetail(false)} />
      )}
    </>
  );
}

function EventTypeBadge({ event, payload }: { event: string; payload: unknown }) {
  let label = event;
  if (event === "agent") {
    const p = payload as { type?: string };
    if (p?.type) label = p.type;
  }

  const variant =
    label.includes("failed") || label.includes("cancelled") || label.includes("deleted")
      ? "destructive"
      : label.includes("completed") || label.includes("created") || label.includes("added")
        ? "success"
        : label.includes("started") || label.includes("progress") || label.includes("claimed")
          ? "info"
          : "secondary";

  return (
    <Badge variant={variant} className="shrink-0 font-mono text-xs">
      {label}
    </Badge>
  );
}

function EventMetadataFooter({ payload }: { payload: unknown }) {
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;

  const channel = p.channel as string | undefined;
  const userId = (p.user_id ?? p.userId) as string | undefined;
  const chatId = (p.chat_id ?? p.chatId) as string | undefined;

  if (!channel && !userId && !chatId) return null;

  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5 pl-[22px] text-xs text-muted-foreground">
      {channel && (
        <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5">
          <Radio className="h-3 w-3" />
          {channel}
        </span>
      )}
      {userId && (
        <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5">
          <User className="h-3 w-3" />
          {userId}
        </span>
      )}
      {chatId && (
        <span className="rounded bg-muted px-1.5 py-0.5 font-mono">
          chat: {String(chatId)}
        </span>
      )}
    </div>
  );
}
