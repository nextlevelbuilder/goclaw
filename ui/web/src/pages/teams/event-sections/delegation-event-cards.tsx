import { Badge } from "@/components/ui/badge";
import { formatDuration } from "@/lib/format";
import type { TeamEventEntry } from "@/stores/use-team-event-store";
import type {
  DelegationEventPayload,
  DelegationProgressPayload,
  DelegationAccumulatedPayload,
  DelegationAnnouncePayload,
  QualityGateRetryPayload,
} from "@/types/team-events";

interface Props {
  entry: TeamEventEntry;
  resolveAgent: (keyOrId: string | undefined) => string;
}

export function DelegationEventCard({ entry, resolveAgent }: Props) {
  switch (entry.event) {
    case "delegation.started":
    case "delegation.completed":
    case "delegation.failed":
    case "delegation.cancelled":
      return <DelegationLifecycleCard entry={entry} resolveAgent={resolveAgent} />;
    case "delegation.progress":
      return <DelegationProgressCard payload={entry.payload as DelegationProgressPayload} resolveAgent={resolveAgent} />;
    case "delegation.accumulated":
      return <DelegationAccumulatedCard payload={entry.payload as DelegationAccumulatedPayload} resolveAgent={resolveAgent} />;
    case "delegation.announce":
      return <DelegationAnnounceCard payload={entry.payload as DelegationAnnouncePayload} resolveAgent={resolveAgent} />;
    case "delegation.quality_gate.retry":
      return <QualityGateRetryCard payload={entry.payload as QualityGateRetryPayload} resolveAgent={resolveAgent} />;
    default:
      return <pre className="overflow-x-auto text-xs">{JSON.stringify(entry.payload, null, 2)}</pre>;
  }
}

function DelegationLifecycleCard({ entry, resolveAgent }: Props) {
  const p = entry.payload as DelegationEventPayload;
  const source = p.source_display_name || resolveAgent(p.source_agent_key);
  const target = p.target_display_name || resolveAgent(p.target_agent_key);
  return (
    <div className="space-y-1 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5">
        <span className="truncate font-medium">{source}</span>
        <span className="shrink-0 text-muted-foreground">&rarr;</span>
        <span className="truncate font-medium">{target}</span>
        {p.mode && (
          <Badge variant="outline" className="shrink-0 text-xs">
            {p.mode}
          </Badge>
        )}
        {p.channel && (
          <Badge variant="outline" className="shrink-0 text-xs">{p.channel}</Badge>
        )}
      </div>
      {p.task && <p className="break-words text-xs text-muted-foreground line-clamp-2">{p.task}</p>}
      <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
        {p.delegation_id && (
          <span className="shrink-0">
            deleg: <span className="font-mono">{p.delegation_id.slice(0, 8)}</span>
          </span>
        )}
        {p.elapsed_ms != null && p.elapsed_ms > 0 && <span>{formatDuration(p.elapsed_ms)}</span>}
        {p.error && <span className="break-words text-destructive">{p.error}</span>}
      </div>
    </div>
  );
}

type ResolverProp = { resolveAgent: (keyOrId: string | undefined) => string };

function DelegationProgressCard({ payload: p, resolveAgent }: { payload: DelegationProgressPayload } & ResolverProp) {
  return (
    <div className="space-y-1 text-sm">
      <p className="truncate text-xs text-muted-foreground">
        {resolveAgent(p.source_agent_key)} has {p.active_delegations.length} active delegation(s)
      </p>
      <div className="space-y-0.5">
        {p.active_delegations.map((d) => (
          <div key={d.delegation_id} className="flex min-w-0 items-center gap-2 text-xs">
            <span className="truncate font-medium">{d.target_display_name || resolveAgent(d.target_agent_key)}</span>
            <span className="shrink-0 text-muted-foreground">{formatDuration(d.elapsed_ms)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function DelegationAccumulatedCard({ payload: p, resolveAgent }: { payload: DelegationAccumulatedPayload } & ResolverProp) {
  return (
    <div className="text-sm">
      <span className="font-medium">{p.target_display_name || resolveAgent(p.target_agent_key)}</span>
      <span className="text-muted-foreground"> result accumulated, </span>
      <span>{p.siblings_remaining} sibling(s) remaining</span>
      {p.elapsed_ms != null && p.elapsed_ms > 0 && (
        <span className="ml-1 text-xs text-muted-foreground">
          ({formatDuration(p.elapsed_ms)})
        </span>
      )}
    </div>
  );
}

function DelegationAnnounceCard({ payload: p, resolveAgent }: { payload: DelegationAnnouncePayload } & ResolverProp) {
  return (
    <div className="space-y-1.5 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5">
        <span className="font-medium">{p.source_display_name || resolveAgent(p.source_agent_key)}</span>
        <span className="text-muted-foreground">announcing {p.results.length} result(s)</span>
        <span className="text-xs text-muted-foreground">({formatDuration(p.total_elapsed_ms)})</span>
        {p.has_media && (
          <Badge variant="outline" className="shrink-0 text-xs">media</Badge>
        )}
        {p.completed_task_ids && p.completed_task_ids.length > 0 && (
          <Badge variant="success" className="shrink-0 text-xs">
            {p.completed_task_ids.length} task(s) auto-completed
          </Badge>
        )}
      </div>
      <div className="space-y-1">
        {p.results.map((r) => (
          <div key={r.agent_key} className="min-w-0 text-xs">
            <div className="flex min-w-0 items-start gap-1.5">
              <span className="shrink-0 font-medium">{r.display_name || resolveAgent(r.agent_key)}</span>
              {r.has_media && (
                <Badge variant="outline" className="shrink-0 text-xs">media</Badge>
              )}
            </div>
            {r.content_preview && (
              <p className="mt-0.5 break-words text-muted-foreground line-clamp-1">{r.content_preview}</p>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function QualityGateRetryCard({ payload: p, resolveAgent }: { payload: QualityGateRetryPayload } & ResolverProp) {
  return (
    <div className="space-y-1 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <span className="truncate font-medium">{resolveAgent(p.target_agent_key)}</span>
        <Badge variant="warning" className="shrink-0 text-xs">
          retry {p.attempt}/{p.max_retries}
        </Badge>
        <Badge variant="outline" className="shrink-0 text-xs">{p.gate_type}</Badge>
      </div>
      {p.feedback && (
        <p className="break-words text-xs text-muted-foreground line-clamp-2">{p.feedback}</p>
      )}
    </div>
  );
}
