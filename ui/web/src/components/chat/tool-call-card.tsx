import { useState } from "react";
import { Wrench, Check, AlertTriangle, Loader2, ChevronDown, ChevronRight, Zap } from "lucide-react";
import type { ToolStreamEntry } from "@/types/chat";

const isSkillTool = (name: string) => name === "use_skill";

/** Build a short summary string from tool arguments for inline display. */
function buildToolSummary(entry: ToolStreamEntry): string | null {
  if (!entry.arguments) return null;
  const args = entry.arguments;
  const key = args.path ?? args.command ?? args.query ?? args.url ?? args.name;
  if (typeof key === "string") return key.length > 60 ? key.slice(0, 57) + "..." : key;
  return null;
}

interface ToolCallCardProps {
  entry: ToolStreamEntry;
}

export function ToolCallCard({ entry }: ToolCallCardProps) {
  const hasDetails = entry.arguments || entry.result;
  const hasError = entry.phase === "error" && !!entry.errorContent;
  const canExpand = hasDetails || hasError;
  const [expanded, setExpanded] = useState(false);
  const summary = buildToolSummary(entry);

  return (
    <div className="my-1 rounded-md border bg-muted/50 text-sm">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-3 py-2 text-left"
        onClick={() => canExpand && setExpanded((v) => !v)}
        disabled={!canExpand}
      >
        <ToolIcon phase={entry.phase} isSkill={isSkillTool(entry.name)} />
        <span className="font-medium shrink-0">
          {isSkillTool(entry.name)
            ? `skill: ${(entry.arguments?.name as string) || "unknown"}`
            : entry.name}
        </span>
        {summary && (
          <span className="truncate text-xs text-muted-foreground">{summary}</span>
        )}
        <span className="ml-auto flex items-center gap-2 shrink-0">
          <PhaseLabel phase={entry.phase} isSkill={isSkillTool(entry.name)} />
          {canExpand && (
            expanded
              ? <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
              : <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
          )}
        </span>
      </button>
      {expanded && canExpand && (
        <div className="border-t border-muted px-3 py-2 space-y-2">
          {hasError && (
            <pre className="text-red-500 whitespace-pre-wrap">{entry.errorContent}</pre>
          )}
          {entry.arguments && Object.keys(entry.arguments).length > 0 && (
            <div>
              <div className="text-[10px] font-semibold uppercase text-muted-foreground mb-1">Arguments</div>
              <pre className="whitespace-pre-wrap text-xs font-mono bg-background rounded p-2 max-h-48 overflow-y-auto">
                {JSON.stringify(entry.arguments, null, 2)}
              </pre>
            </div>
          )}
          {entry.result && (
            <div>
              <div className="text-[10px] font-semibold uppercase text-muted-foreground mb-1">Result</div>
              <pre className="whitespace-pre-wrap text-xs font-mono bg-background rounded p-2 max-h-48 overflow-y-auto">
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
  if (isSkill) {
    switch (phase) {
      case "calling":
        return <Zap className="h-4 w-4 animate-pulse text-amber-500" />;
      case "completed":
        return <Zap className="h-4 w-4 text-amber-500" />;
      case "error":
        return <AlertTriangle className="h-4 w-4 text-red-500" />;
      default:
        return <Zap className="h-4 w-4 text-muted-foreground" />;
    }
  }
  switch (phase) {
    case "calling":
      return <Loader2 className="h-4 w-4 animate-spin text-blue-500" />;
    case "completed":
      return <Check className="h-4 w-4 text-green-500" />;
    case "error":
      return <AlertTriangle className="h-4 w-4 text-red-500" />;
    default:
      return <Wrench className="h-4 w-4 text-muted-foreground" />;
  }
}

function PhaseLabel({ phase, isSkill }: { phase: ToolStreamEntry["phase"]; isSkill?: boolean }) {
  const labels: Record<string, string> = isSkill
    ? { calling: "Activating...", completed: "Activated", error: "Failed" }
    : { calling: "Running...", completed: "Done", error: "Failed" };
  const colors: Record<string, string> = {
    calling: "text-blue-500",
    completed: "text-green-500",
    error: "text-red-500",
  };
  return (
    <span className={`text-xs ${colors[phase] ?? "text-muted-foreground"}`}>
      {labels[phase] ?? phase}
    </span>
  );
}
