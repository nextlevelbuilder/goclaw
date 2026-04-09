import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Wrench, AlertTriangle, ChevronRight, Zap } from "lucide-react";
import type { ToolStreamEntry } from "@/types/chat";

const isSkillTool = (name: string) => name === "use_skill";

/** Build a short summary string from tool arguments for inline display. */
function buildToolSummary(entry: ToolStreamEntry): string | null {
  if (!entry.arguments) return null;
  const args = entry.arguments;
  const key = args.path ?? args.command ?? args.query ?? args.url ?? args.name;
  if (typeof key === "string") return key.length > 80 ? key.slice(0, 77) + "..." : key;
  return null;
}

interface ToolCallCardProps {
  entry: ToolStreamEntry;
  /** Compact mode — less padding, used inside merged groups */
  compact?: boolean;
}

export function ToolCallCard({ entry, compact }: ToolCallCardProps) {
  const { t } = useTranslation("common");
  const hasDetails = entry.arguments || entry.result;
  const hasError = entry.phase === "error" && !!entry.errorContent;
  const canExpand = hasDetails || hasError;
  const [expanded, setExpanded] = useState(false);
  const summary = buildToolSummary(entry);
  const skill = isSkillTool(entry.name);
  const displayName = skill ? `skill: ${(entry.arguments?.name as string) || "unknown"}` : entry.name;

  return (
    <div className={compact ? "" : "rounded-md border bg-muted"}>
      <button
        type="button"
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs"
        onClick={() => canExpand && setExpanded((v) => !v)}
        disabled={!canExpand}
      >
        <ToolIcon phase={entry.phase} isSkill={skill} />
        <span className="font-medium shrink-0">{displayName}</span>
        {summary && <span className="truncate text-muted-foreground ml-1">{summary}</span>}
        <span className="ml-auto flex items-center gap-1 shrink-0">
          <PhaseLabel phase={entry.phase} isSkill={skill} />
          {canExpand && (
            <ChevronRight className={`h-3 w-3 text-muted-foreground transition-transform ${expanded ? "rotate-90" : ""}`} />
          )}
        </span>
      </button>
      {expanded && canExpand && (
        <div className="border-t border-muted px-2 py-1.5 space-y-1.5">
          {hasError && (
            <pre className="text-red-500 whitespace-pre-wrap text-xs">{entry.errorContent}</pre>
          )}
          {entry.arguments && Object.keys(entry.arguments).length > 0 && (
            <div>
              <div className="text-2xs font-semibold uppercase text-muted-foreground mb-0.5">{t("toolArguments")}</div>
              <pre className="whitespace-pre-wrap text-xs-plus font-mono bg-background rounded p-1.5 max-h-40 overflow-y-auto">
                {JSON.stringify(entry.arguments, null, 2)}
              </pre>
            </div>
          )}
          {entry.result && (
            <div>
              <div className="text-2xs font-semibold uppercase text-muted-foreground mb-0.5">{t("toolResult")}</div>
              <pre className="whitespace-pre-wrap text-xs-plus font-mono bg-background rounded p-1.5 max-h-40 overflow-y-auto">
                {entry.result}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ToolIcon({ phase, isSkill }: { phase: ToolStreamEntry["phase"]; isSkill?: boolean }) {
  const cls = "h-3.5 w-3.5";
  if (isSkill) {
    switch (phase) {
      case "calling": return <Zap className={`${cls} animate-pulse text-amber-500`} />;
      case "completed": return <Zap className={`${cls} text-amber-500`} />;
      case "error": return <AlertTriangle className={`${cls} text-red-500`} />;
      default: return <Zap className={`${cls} text-muted-foreground`} />;
    }
  }
  switch (phase) {
    case "calling": return <Wrench className={`${cls} animate-wobble text-blue-500`} />;
    case "completed": return <Wrench className={`${cls} text-blue-500`} />;
    case "error": return <AlertTriangle className={`${cls} text-red-500`} />;
    default: return <Wrench className={`${cls} text-muted-foreground`} />;
  }
}

function PhaseLabel({ phase, isSkill }: { phase: ToolStreamEntry["phase"]; isSkill?: boolean }) {
  const { t } = useTranslation("common");
  const labels: Record<string, string> = isSkill
    ? { calling: t("skillActivating"), completed: t("skillActivated"), error: t("toolFailed") }
    : { calling: t("toolRunning"), completed: t("toolDone"), error: t("toolFailed") };
  const colors: Record<string, string> = {
    calling: "text-blue-500",
    completed: "text-blue-500",
    error: "text-red-500",
  };
  return <span className={`text-xs-plus ${colors[phase] ?? "text-muted-foreground"}`}>{labels[phase] ?? phase}</span>;
}
