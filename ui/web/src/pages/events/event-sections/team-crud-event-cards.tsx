import { Badge } from "@/components/ui/badge";
import type { TeamEventEntry } from "@/stores/use-team-event-store";
import type {
  TeamCreatedPayload,
  TeamUpdatedPayload,
  TeamDeletedPayload,
  TeamMemberAddedPayload,
  TeamMemberRemovedPayload,
  AgentLinkCreatedPayload,
  AgentLinkUpdatedPayload,
  AgentLinkDeletedPayload,
} from "@/types/team-events";

interface Props {
  entry: TeamEventEntry;
  resolveAgent: (keyOrId: string | undefined) => string;
}

export function TeamCrudEventCard({ entry, resolveAgent }: Props) {
  switch (entry.event) {
    case "team.created":
      return <TeamCreatedCard payload={entry.payload as TeamCreatedPayload} resolveAgent={resolveAgent} />;
    case "team.updated":
      return <TeamUpdatedCard payload={entry.payload as TeamUpdatedPayload} />;
    case "team.deleted":
      return <TeamDeletedCard payload={entry.payload as TeamDeletedPayload} />;
    case "team.member.added":
      return <MemberAddedCard payload={entry.payload as TeamMemberAddedPayload} resolveAgent={resolveAgent} />;
    case "team.member.removed":
      return <MemberRemovedCard payload={entry.payload as TeamMemberRemovedPayload} resolveAgent={resolveAgent} />;
    case "agent_link.created":
      return <LinkCreatedCard payload={entry.payload as AgentLinkCreatedPayload} resolveAgent={resolveAgent} />;
    case "agent_link.updated":
      return <LinkUpdatedCard payload={entry.payload as AgentLinkUpdatedPayload} resolveAgent={resolveAgent} />;
    case "agent_link.deleted":
      return <LinkDeletedCard payload={entry.payload as AgentLinkDeletedPayload} resolveAgent={resolveAgent} />;
    default:
      return <pre className="overflow-x-auto text-xs">{JSON.stringify(entry.payload, null, 2)}</pre>;
  }
}

type R = { resolveAgent: (keyOrId: string | undefined) => string };

function TeamCreatedCard({ payload: p, resolveAgent }: { payload: TeamCreatedPayload } & R) {
  return (
    <div className="text-sm">
      <span>Team </span>
      <span className="font-medium">{p.team_name}</span>
      <span> created</span>
      {p.lead_agent_key && (
        <span className="text-muted-foreground">
          {" "}(lead: {p.lead_display_name || resolveAgent(p.lead_agent_key)})
        </span>
      )}
      <span className="ml-1 text-xs text-muted-foreground">{p.member_count} member(s)</span>
    </div>
  );
}

function TeamUpdatedCard({ payload: p }: { payload: TeamUpdatedPayload }) {
  return (
    <div className="text-sm">
      <span>Team </span>
      <span className="font-medium">{p.team_name}</span>
      <span> updated</span>
      {p.changes?.length > 0 && (
        <span className="ml-1 text-xs text-muted-foreground">({p.changes.join(", ")})</span>
      )}
    </div>
  );
}

function TeamDeletedCard({ payload: p }: { payload: TeamDeletedPayload }) {
  return (
    <div className="text-sm text-destructive">
      <span>Team </span>
      <span className="font-medium">{p.team_name}</span>
      <span> deleted</span>
    </div>
  );
}

function MemberAddedCard({ payload: p, resolveAgent }: { payload: TeamMemberAddedPayload } & R) {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5 text-sm">
      <span className="truncate font-medium">{p.display_name || resolveAgent(p.agent_key)}</span>
      <span className="shrink-0 text-muted-foreground">added to</span>
      <span className="truncate font-medium">{p.team_name}</span>
      <Badge variant="outline" className="shrink-0 text-xs">{p.role}</Badge>
    </div>
  );
}

function MemberRemovedCard({ payload: p, resolveAgent }: { payload: TeamMemberRemovedPayload } & R) {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5 text-sm">
      <span className="truncate font-medium">{p.display_name || resolveAgent(p.agent_key)}</span>
      <span className="shrink-0 text-muted-foreground">removed from</span>
      <span className="truncate font-medium">{p.team_name}</span>
    </div>
  );
}

function LinkCreatedCard({ payload: p, resolveAgent }: { payload: AgentLinkCreatedPayload } & R) {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5 text-sm">
      <span className="truncate font-medium">{resolveAgent(p.source_agent_key)}</span>
      <span className="shrink-0 text-muted-foreground">&rarr;</span>
      <span className="truncate font-medium">{resolveAgent(p.target_agent_key)}</span>
      <Badge variant="outline" className="shrink-0 text-xs">{p.direction}</Badge>
      <Badge variant="success" className="shrink-0 text-xs">{p.status}</Badge>
    </div>
  );
}

function LinkUpdatedCard({ payload: p, resolveAgent }: { payload: AgentLinkUpdatedPayload } & R) {
  return (
    <div className="text-sm">
      <span>Link </span>
      <span className="font-medium">
        {resolveAgent(p.source_agent_key)} &rarr; {resolveAgent(p.target_agent_key)}
      </span>
      {p.changes?.length > 0 && (
        <span className="ml-1 text-xs text-muted-foreground">({p.changes.join(", ")})</span>
      )}
    </div>
  );
}

function LinkDeletedCard({ payload: p, resolveAgent }: { payload: AgentLinkDeletedPayload } & R) {
  return (
    <div className="text-sm text-destructive">
      <span>Link </span>
      <span className="font-medium">
        {resolveAgent(p.source_agent_key)} &rarr; {resolveAgent(p.target_agent_key)}
      </span>
      <span> deleted</span>
    </div>
  );
}
