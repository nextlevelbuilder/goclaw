import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Wrench, AlertTriangle, ChevronRight, Zap, CircleDot } from "lucide-react";
import type { ToolStreamEntry } from "@/types/chat";

const isSkillTool = (name: string) => name === "use_skill";
type ProgressHistoryItem = NonNullable<ToolStreamEntry["progressHistory"]>[number];

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
  const hasProgressEventData = !!entry.progressEvent || !!entry.progressRunId || !!entry.progressTimestamp || !!entry.progressEventData;
  const history = entry.progressHistory ?? [];
  const hasError = entry.phase === "error" && !!entry.errorContent;
  const canExpand = hasDetails || hasError || hasProgressEventData || history.length > 0;
  const [expanded, setExpanded] = useState(false);
  const summary = buildToolSummary(entry);
  const skill = isSkillTool(entry.name);
  const displayName = skill ? `skill: ${(entry.arguments?.name as string) || "unknown"}` : entry.name;
  const progressRatio = entry.progressTotal ? `${entry.progress ?? 0}/${entry.progressTotal}` : "";
  const visibleHistory = history.map(toDisplayEvent).filter((item) => item.visible).slice(-8);

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
        {entry.phase === "calling" && entry.progressMessage && (
          <span className="min-w-0 truncate text-blue-500 ml-1">
            {entry.progressMessage}
          </span>
        )}
        <span className="ml-auto flex items-center gap-1 shrink-0">
          {entry.phase === "calling" && progressRatio && (
            <span className="text-xs-plus text-blue-500">{progressRatio}</span>
          )}
          <PhaseLabel phase={entry.phase} isSkill={skill} />
          {canExpand && (
            <ChevronRight className={`h-3 w-3 text-muted-foreground transition-transform ${expanded ? "rotate-90" : ""}`} />
          )}
        </span>
      </button>
      {visibleHistory.length > 0 && (
        <div className="border-t border-muted bg-background/60 px-3 py-2">
          <div className="space-y-1.5">
            {visibleHistory.map((item, idx) => (
              <div key={`${item.timestamp ?? "evt"}-${idx}`} className="grid grid-cols-[16px_1fr_auto] gap-2 text-xs">
                <CircleDot className={`mt-0.5 h-3 w-3 ${item.accent}`} />
                <div className="min-w-0">
                  <div className="truncate font-medium text-foreground">{item.title}</div>
                  {item.detail && <div className="truncate text-muted-foreground">{item.detail}</div>}
                </div>
                {item.ratio && <div className="text-xs-plus text-muted-foreground">{item.ratio}</div>}
              </div>
            ))}
          </div>
        </div>
      )}
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
          {history.length > 0 && (
            <div>
              <div className="text-2xs font-semibold uppercase text-muted-foreground mb-0.5">Event Trace</div>
              <pre className="whitespace-pre-wrap text-xs-plus font-mono bg-background rounded p-1.5 max-h-72 overflow-y-auto">
                {JSON.stringify(history.map(toRawEvent), null, 2)}
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

function toDisplayEvent(item: ProgressHistoryItem) {
  const data = progressEventData(item);
  const event = progressEventName(item);
  const ratio = typeof item.progress === "number" && typeof item.total === "number" ? `${item.progress}/${item.total}` : "";
  const action = stringValue(data.last_action);
  const excerpt = stringValue(data.last_excerpt);
  const step = numberOrString(data.step);
  const phase = stringValue(data.phase);

  if (event === "heartbeat") {
    return { visible: false, title: "", accent: "text-muted-foreground" };
  }

  const payload = asRecord(data.payload);
  const payloadTitle = stringValue(payload?.title);
  const payloadText = stringValue(payload?.text);
  if (payloadTitle || payloadText) {
    return {
      visible: true,
      title: payloadTitle || event,
      detail: payloadText,
      ratio,
      accent: event === "run_failed" ? "text-red-500" : event === "run_completed" ? "text-emerald-500" : "text-blue-500",
      timestamp: item.timestamp,
    };
  }

  if (event === "step_start") {
    return { visible: false, title: "", accent: "text-muted-foreground" };
  }
  if (event === "step_end") {
    if (action === "wait") {
      return { visible: false, title: "", accent: "text-muted-foreground" };
    }
    const translated = translateBrowserAction(action, excerpt);
    return {
      visible: true,
      title: translated.title,
      detail: translated.detail || (step ? `浏览器步骤 ${step} 完成` : ""),
      ratio,
      accent: action === "click" ? "text-emerald-500" : "text-blue-500",
      timestamp: item.timestamp,
    };
  }

  const message = item.message || stringValue(data.message);
  if (event === "run_started") {
    return { visible: true, title: message || "任务已开始", detail: stringValue(data.form_url), ratio, accent: "text-blue-500", timestamp: item.timestamp };
  }
  if (event === "phase_started") {
    return { visible: true, title: message || (phase ? `进入 ${phase} 阶段` : "进入处理阶段"), detail: phase, ratio, accent: "text-blue-500", timestamp: item.timestamp };
  }
  if (event === "phase_completed") {
    return { visible: true, title: message || (phase ? `${phase} 阶段完成` : "阶段完成"), detail: phase, ratio, accent: "text-emerald-500", timestamp: item.timestamp };
  }
  if (event === "user_response_received") {
    const intent = asRecord(data.intent);
    return {
      visible: true,
      title: message || "已收到用户回复",
      detail: stringValue(intent?.intent) || stringValue(intent?.reason),
      ratio,
      accent: "text-emerald-500",
      timestamp: item.timestamp,
    };
  }
  if (event === "run_failed") {
    return { visible: true, title: message || "任务失败", detail: stringValue(data.error), ratio, accent: "text-red-500", timestamp: item.timestamp };
  }
  if (event === "run_completed") {
    return { visible: true, title: message || "任务完成", detail: stringValue(data.final_text) || stringValue(data.submission_result), ratio, accent: "text-emerald-500", timestamp: item.timestamp };
  }

  return { visible: true, title: message || event, detail: event, ratio, accent: "text-muted-foreground", timestamp: item.timestamp };
}

function translateBrowserAction(action: string, excerpt: string) {
  if (action === "input") {
    const typed = excerpt.match(/Typed '([^']*)'/)?.[1];
    return { title: typed ? `已输入：${typed}` : "已填写表单内容", detail: excerpt };
  }
  if (action === "click") {
    if (/submit/i.test(excerpt)) return { title: "已点击提交", detail: excerpt };
    return { title: "已点击页面按钮", detail: excerpt };
  }
  if (action === "wait") {
    return { title: "等待页面响应", detail: excerpt };
  }
  if (action === "done") {
    return { title: "浏览器任务完成", detail: trimText(excerpt, 120) };
  }
  if (action.includes("guard_status")) {
    return { title: "校验提交状态", detail: trimText(excerpt, 120) };
  }
  return { title: action ? `浏览器动作：${action}` : "完成一个浏览器步骤", detail: trimText(excerpt, 120) };
}

function toRawEvent(item: ProgressHistoryItem) {
  const envelope = asRecord(item.eventData);
  if (envelope && (typeof envelope.event === "string" || envelope.data)) {
    return envelope;
  }
  return {
    event: item.event,
    run_id: item.runId,
    timestamp: item.timestamp,
    progress: item.progress,
    total: item.total,
    message: item.message,
    data: item.eventData,
  };
}

function progressEventName(item: ProgressHistoryItem): string {
  const envelope = asRecord(item.eventData);
  return stringValue(envelope?.event) || item.event || "event";
}

function progressEventData(item: ProgressHistoryItem): Record<string, unknown> {
  const envelope = asRecord(item.eventData);
  const nested = asRecord(envelope?.data);
  if (nested) return nested;
  return envelope ?? {};
}

function asRecord(v: unknown): Record<string, unknown> | undefined {
  if (v && typeof v === "object" && !Array.isArray(v)) return v as Record<string, unknown>;
  return undefined;
}

function stringValue(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function numberOrString(v: unknown): string {
  if (typeof v === "number") return String(v);
  if (typeof v === "string") return v;
  return "";
}

function trimText(s: string, max: number): string {
  if (s.length <= max) return s;
  return `${s.slice(0, max - 3)}...`;
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
