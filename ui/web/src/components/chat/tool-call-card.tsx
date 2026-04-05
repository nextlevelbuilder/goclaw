import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Wrench, AlertTriangle, Loader2, ChevronDown, ChevronRight, Zap, Monitor } from "lucide-react";
import type { ToolStreamEntry } from "@/types/chat";
import { useBrowserViewStore } from "@/stores/use-browser-view-store";

const isSkillTool = (name: string) => name === "use_skill";
const isBrowserTool = (name: string) => name === "browser";

/** Extract targetId from browser tool result or arguments. */
function extractBrowserTarget(entry: ToolStreamEntry): string | null {
  // Try to get targetId from result JSON
  if (entry.result) {
    try {
      const parsed = JSON.parse(entry.result);
      if (parsed.targetId) return parsed.targetId;
      if (parsed.targetID) return parsed.targetID;
    } catch { /* not JSON, try regex */ }
    const match = entry.result.match(/targetId["\s:]+["']?([A-F0-9]{32})/i);
    if (match?.[1]) return match[1];
  }
  // Fallback: targetId in arguments
  const argTarget = entry.arguments?.targetId ?? entry.arguments?.targetID;
  if (typeof argTarget === "string" && argTarget) return argTarget;
  return null;
}

/** Build a short summary string from tool arguments for inline display. */
function buildToolSummary(entry: ToolStreamEntry): string | null {
  if (!entry.arguments) return null;
  const args = entry.arguments;
  // For browser tool, show the action
  if (isBrowserTool(entry.name) && args.action) {
    const url = args.targetUrl ?? args.url;
    return typeof url === "string" ? `${args.action}: ${url}` : String(args.action);
  }
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
  const openBrowserView = useBrowserViewStore((s) => s.openBrowserView);
  const hasDetails = entry.arguments || entry.result;
  const hasError = entry.phase === "error" && !!entry.errorContent;
  const canExpand = hasDetails || hasError;
  const [expanded, setExpanded] = useState(false);
  const summary = buildToolSummary(entry);
  const skill = isSkillTool(entry.name);
  const browser = isBrowserTool(entry.name);
  const browserTarget = browser ? extractBrowserTarget(entry) : null;
  const displayName = skill ? `skill: ${(entry.arguments?.name as string) || "unknown"}` : entry.name;

  return (
    <div className={compact ? "" : "rounded-md border bg-muted"}>
      <button
        type="button"
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs"
        onClick={() => canExpand && setExpanded((v) => !v)}
        disabled={!canExpand}
      >
        <ToolIcon phase={entry.phase} isSkill={skill} isBrowser={browser} />
        <span className="font-medium shrink-0">{displayName}</span>
        {summary && <span className="truncate text-muted-foreground ml-1">{summary}</span>}
        <span className="ml-auto flex items-center gap-1 shrink-0">
          {browserTarget && entry.phase !== "error" && (
            <span
              role="button"
              tabIndex={0}
              className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[11px] font-medium text-emerald-600 hover:bg-emerald-500/10 dark:text-emerald-400"
              onClick={(e) => {
                e.stopPropagation();
                const url = entry.arguments?.targetUrl ?? entry.arguments?.url;
                openBrowserView(browserTarget, undefined, typeof url === "string" ? url : undefined);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.stopPropagation();
                  openBrowserView(browserTarget);
                }
              }}
            >
              <Monitor className="h-3 w-3" /> View
            </span>
          )}
          <PhaseLabel phase={entry.phase} isSkill={skill} />
          {canExpand && (
            expanded
              ? <ChevronDown className="h-3 w-3 text-muted-foreground" />
              : <ChevronRight className="h-3 w-3 text-muted-foreground" />
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
              <div className="text-[10px] font-semibold uppercase text-muted-foreground mb-0.5">{t("toolArguments")}</div>
              <pre className="whitespace-pre-wrap text-[11px] font-mono bg-background rounded p-1.5 max-h-40 overflow-y-auto">
                {JSON.stringify(entry.arguments, null, 2)}
              </pre>
            </div>
          )}
          {entry.result && (
            <div>
              <div className="text-[10px] font-semibold uppercase text-muted-foreground mb-0.5">{t("toolResult")}</div>
              <pre className="whitespace-pre-wrap text-[11px] font-mono bg-background rounded p-1.5 max-h-40 overflow-y-auto">
                {entry.result}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ToolIcon({ phase, isSkill, isBrowser }: { phase: ToolStreamEntry["phase"]; isSkill?: boolean; isBrowser?: boolean }) {
  const cls = "h-3.5 w-3.5";
  if (isSkill) {
    switch (phase) {
      case "calling": return <Zap className={`${cls} animate-pulse text-amber-500`} />;
      case "completed": return <Zap className={`${cls} text-amber-500`} />;
      case "error": return <AlertTriangle className={`${cls} text-red-500`} />;
      default: return <Zap className={`${cls} text-muted-foreground`} />;
    }
  }
  if (isBrowser) {
    switch (phase) {
      case "calling": return <Monitor className={`${cls} animate-pulse text-emerald-500`} />;
      case "completed": return <Monitor className={`${cls} text-emerald-500`} />;
      case "error": return <AlertTriangle className={`${cls} text-red-500`} />;
      default: return <Monitor className={`${cls} text-muted-foreground`} />;
    }
  }
  switch (phase) {
    case "calling": return <Loader2 className={`${cls} animate-spin text-blue-500`} />;
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
  return <span className={`text-[11px] ${colors[phase] ?? "text-muted-foreground"}`}>{labels[phase] ?? phase}</span>;
}
