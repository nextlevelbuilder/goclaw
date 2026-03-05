import { Badge } from "@/components/ui/badge";
import type { TeamEventEntry } from "@/stores/use-team-event-store";
import type { EnrichedAgentEventPayload } from "@/types/team-events";

interface Props {
  entry: TeamEventEntry;
  resolveAgent: (keyOrId: string | undefined) => string;
}

export function AgentEventCard({ entry, resolveAgent }: Props) {
  const p = entry.payload as EnrichedAgentEventPayload;
  const subtype = p.type;

  if (subtype === "tool.call" || subtype === "tool.result") {
    return <ToolEventCard p={p} resolveAgent={resolveAgent} />;
  }

  return <RunEventCard p={p} resolveAgent={resolveAgent} />;
}

/** run.started / run.completed / run.failed / run.retrying */
function RunEventCard({ p, resolveAgent }: { p: EnrichedAgentEventPayload; resolveAgent: Props["resolveAgent"] }) {
  const subtype = p.type;
  const variant =
    subtype === "run.completed"
      ? ("success" as const)
      : subtype === "run.failed"
        ? ("destructive" as const)
        : subtype === "run.retrying"
          ? ("warning" as const)
          : ("info" as const);

  return (
    <div className="space-y-1 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5">
        <Badge variant={variant} className="shrink-0 font-mono text-xs">
          {subtype}
        </Badge>
        <span className="truncate font-medium">{resolveAgent(p.agentId)}</span>
      </div>

      <ContextRow p={p} resolveAgent={resolveAgent} />

      {subtype === "run.failed" && p.payload?.error && (
        <p className="break-words text-xs text-destructive line-clamp-2">{p.payload.error}</p>
      )}
    </div>
  );
}

/** tool.call / tool.result */
function ToolEventCard({ p, resolveAgent }: { p: EnrichedAgentEventPayload; resolveAgent: Props["resolveAgent"] }) {
  const isResult = p.type === "tool.result";
  const toolName = p.payload?.name;
  const isError = isResult && p.payload?.is_error;

  return (
    <div className="space-y-1 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5">
        <Badge variant={isError ? "destructive" : "secondary"} className="shrink-0 font-mono text-xs">
          {p.type}
        </Badge>
        {toolName && (
          <span className="truncate font-mono font-medium">{toolName}</span>
        )}
        {isResult && (
          <Badge variant={isError ? "destructive" : "success"} className="shrink-0 text-xs">
            {isError ? "error" : "ok"}
          </Badge>
        )}
        <span className="shrink-0 text-muted-foreground">&rarr;</span>
        <span className="truncate">{resolveAgent(p.agentId)}</span>
      </div>

      <ContextRow p={p} resolveAgent={resolveAgent} showCallId />
    </div>
  );
}

/** Shared context row: channel, delegation, parent, team task, call ID */
function ContextRow({
  p,
  resolveAgent,
  showCallId,
}: {
  p: EnrichedAgentEventPayload;
  resolveAgent: Props["resolveAgent"];
  showCallId?: boolean;
}) {
  const hasContext = p.channel || p.delegationId || p.parentAgentId || p.teamTaskId || (showCallId && p.payload?.id);
  if (!hasContext) return null;

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
      {p.channel && (
        <Badge variant="outline" className="shrink-0 text-xs">{p.channel}</Badge>
      )}
      {p.delegationId && (
        <ShortId label="deleg" id={p.delegationId} />
      )}
      {p.parentAgentId && (
        <span className="truncate">
          parent: <span className="font-medium text-foreground">{resolveAgent(p.parentAgentId)}</span>
        </span>
      )}
      {p.teamTaskId && (
        <ShortId label="task" id={p.teamTaskId} />
      )}
      {showCallId && p.payload?.id && (
        <ShortId label="call" id={p.payload.id} />
      )}
    </div>
  );
}

function ShortId({ label, id }: { label: string; id: string }) {
  return (
    <span className="shrink-0 text-xs text-muted-foreground">
      {label}: <span className="font-mono">{id.slice(0, 8)}</span>
    </span>
  );
}
