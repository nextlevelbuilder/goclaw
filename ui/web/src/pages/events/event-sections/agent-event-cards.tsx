import { useTranslation } from "react-i18next";
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
  const message = p.payload?.message;
  const content = p.payload?.content;

  return (
    <div className="space-y-1 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5">
        <span className="truncate font-medium">{resolveAgent(p.agentId)}</span>
        {p.runKind && <RunKindBadge kind={p.runKind} />}
      </div>

      {message && (
        <p className="break-words text-xs text-muted-foreground line-clamp-2">{message}</p>
      )}
      {content && (
        <p className="break-words text-xs text-muted-foreground line-clamp-2">{content}</p>
      )}

      <ContextRow p={p} resolveAgent={resolveAgent} />

      {p.type === "run.failed" && p.payload?.error && (
        <p className="break-words text-xs text-destructive line-clamp-2">{p.payload.error}</p>
      )}
    </div>
  );
}

/** tool.call / tool.result */
function ToolEventCard({ p, resolveAgent }: { p: EnrichedAgentEventPayload; resolveAgent: Props["resolveAgent"] }) {
  const { t } = useTranslation("events");
  const isResult = p.type === "tool.result";
  const toolName = p.payload?.name;
  const isError = isResult && p.payload?.is_error;
  const args = p.payload?.arguments;
  const agentName = resolveAgent(p.agentId);
  const isSkill = toolName === "use_skill";
  const skillName = isSkill ? (args?.name as string) || "unknown" : null;

  // Display label: show "skill: <name>" for use_skill, raw tool name otherwise
  const displayName = isSkill ? `skill: ${skillName}` : toolName;

  return (
    <div className="space-y-1 text-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5">
        {isResult ? (
          <>
            {displayName && (
              <span className={`truncate font-mono font-medium ${isSkill ? "text-amber-600 dark:text-amber-400" : ""}`}>
                {displayName}
              </span>
            )}
            <span className="shrink-0 text-muted-foreground">&rarr;</span>
            <span className="truncate font-medium">{agentName}</span>
            <Badge variant={isError ? "destructive" : isSkill ? "warning" : "success"} className="shrink-0 text-xs">
              {isError ? t("toolBadge.error") : isSkill ? t("toolBadge.activated") : t("toolBadge.ok")}
            </Badge>
            {p.runKind && <RunKindBadge kind={p.runKind} />}
          </>
        ) : (
          <>
            <span className="truncate font-medium">{agentName}</span>
            <span className="shrink-0 text-muted-foreground">&rarr;</span>
            {displayName && (
              <span className={`truncate font-mono font-medium ${isSkill ? "text-amber-600 dark:text-amber-400" : ""}`}>
                {displayName}
              </span>
            )}
            {p.runKind && <RunKindBadge kind={p.runKind} />}
          </>
        )}
      </div>

      {/* Structured tool arguments (only for tool.call, skip for use_skill) */}
      {!isResult && args && !isSkill && <ToolArgs args={args} />}

      <ContextRow p={p} resolveAgent={resolveAgent} showCallId />
    </div>
  );
}

/** Render tool arguments as structured key-value pairs */
function ToolArgs({ args }: { args: Record<string, unknown> }) {
  const { t } = useTranslation("events");
  const entries = Object.entries(args);
  if (entries.length === 0) return null;

  const MAX_DISPLAY = 3;
  const visible = entries.slice(0, MAX_DISPLAY);
  const remaining = entries.length - MAX_DISPLAY;

  return (
    <div className="space-y-0.5 text-xs">
      {visible.map(([key, value]) => (
        <div key={key} className="flex min-w-0 items-baseline gap-1.5">
          <span className="shrink-0 text-muted-foreground">{key}:</span>
          <span className="min-w-0 truncate font-mono text-foreground/80">
            {typeof value === "string" ? value : JSON.stringify(value)}
          </span>
        </div>
      ))}
      {remaining > 0 && (
        <span className="text-muted-foreground">{t("moreArgs", { count: remaining })}</span>
      )}
    </div>
  );
}

/** Shared context row: runId, delegation, parent, team task, call ID */
function ContextRow({
  p,
  resolveAgent,
  showCallId,
}: {
  p: EnrichedAgentEventPayload;
  resolveAgent: Props["resolveAgent"];
  showCallId?: boolean;
}) {
  const { t } = useTranslation("events");
  const hasContext = p.runId || p.delegationId || p.parentAgentId || p.teamTaskId || (showCallId && p.payload?.id);
  if (!hasContext) return null;

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
      {p.delegationId && <PillId label="deleg" id={p.delegationId} />}
      {p.parentAgentId && (
        <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5">
          {t("parentLabel")} <span className="font-medium text-foreground">{resolveAgent(p.parentAgentId)}</span>
        </span>
      )}
      {p.teamTaskId && <PillId label="task" id={p.teamTaskId} />}
      {showCallId && p.payload?.id && <PillId label="call" id={p.payload.id} />}
      {p.runId && <PillId label="run" id={p.runId} />}
    </div>
  );
}

function PillId({ label, id }: { label: string; id: string }) {
  return (
    <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
      {label}: {id.slice(0, 8)}
    </span>
  );
}

function RunKindBadge({ kind }: { kind: string }) {
  const colors: Record<string, string> = {
    delegation: "bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300",
    announce: "bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300",
  };
  return (
    <span className={`shrink-0 rounded px-1.5 py-0.5 text-xs font-medium ${colors[kind] ?? "bg-muted text-muted-foreground"}`}>
      {kind}
    </span>
  );
}
