import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import {
  Activity,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock,
  ExternalLink,
  MessageSquareText,
  Wrench,
  XCircle,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatDate } from "@/lib/format";
import type { RunTimelineItem, RunTimelineItemType } from "@/types/run-timeline";

interface RunTimelinePanelProps {
  items: RunTimelineItem[];
  loading?: boolean;
}

export function RunTimelinePanel({ items, loading }: RunTimelinePanelProps) {
  const { t } = useTranslation("sessions");
  const [open, setOpen] = useState(false);

  if (!loading && items.length === 0) return null;

  return (
    <section className="border-b px-6 py-3">
      <Button
        type="button"
        variant="ghost"
        className="h-8 w-full justify-start gap-2 px-0 text-sm"
        onClick={() => setOpen((v) => !v)}
      >
        {open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
        <Clock className="h-4 w-4" />
        <span className="font-medium">{t("detail.timeline.title")}</span>
        <Badge variant="outline" className="ml-auto">
          {loading ? t("detail.timeline.loading") : t("detail.timeline.count", { count: items.length })}
        </Badge>
      </Button>

      {open && (
        <div className="mt-3 space-y-0">
          {items.map((item, index) => {
            const display = getRunTimelineDisplay(item);
            const Icon = display.icon;
            return (
              <div key={item.id || `${item.run_id}-${item.seq}`} className="relative flex gap-3 pb-4 last:pb-0">
                <div className="flex flex-col items-center">
                  <div className={`mt-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-full ${display.dotClass}`}>
                    <Icon className="h-3.5 w-3.5" />
                  </div>
                  {index < items.length - 1 && <div className="w-px flex-1 bg-border" />}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="outline" className="text-2xs">
                      {t(display.labelKey)}
                    </Badge>
                    <span className="truncate text-sm font-medium">{item.title || t(display.labelKey)}</span>
                    {item.status && <span className="text-xs text-muted-foreground">{item.status}</span>}
                  </div>
                  {item.preview && (
                    <pre className="mt-1 max-h-28 overflow-auto whitespace-pre-wrap break-words rounded bg-muted/50 px-2 py-1 text-xs text-muted-foreground">
                      {item.preview}
                    </pre>
                  )}
                  <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <span>{formatDate(item.created_at)}</span>
                    {item.tool_call_id && <span>{item.tool_call_id}</span>}
                    {item.trace_id && (
                      <Button asChild variant="ghost" size="sm" className="h-6 gap-1 px-1 text-xs">
                        <Link to={`/traces/${item.trace_id}`}>
                          <ExternalLink className="h-3 w-3" />
                          {t("detail.timeline.trace")}
                        </Link>
                      </Button>
                    )}
                    {item.run_id && <span>{item.run_id}</span>}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

export function getRunTimelineDisplay(item: Pick<RunTimelineItem, "item_type" | "status">) {
  if (item.status === "failed") {
    return {
      icon: XCircle,
      dotClass: "bg-destructive/10 text-destructive",
      labelKey: "detail.timeline.failed",
    };
  }
  if (item.item_type === "tool.call" || item.item_type === "tool.result") {
    return {
      icon: Wrench,
      dotClass: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
      labelKey: item.item_type === "tool.call" ? "detail.timeline.toolCall" : "detail.timeline.toolResult",
    };
  }
  if (item.item_type === "assistant.message") {
    return {
      icon: MessageSquareText,
      dotClass: "bg-blue-500/10 text-blue-700 dark:text-blue-300",
      labelKey: "detail.timeline.assistant",
    };
  }
  if (item.item_type === "activity") {
    return {
      icon: Activity,
      dotClass: "bg-primary/10 text-primary",
      labelKey: "detail.timeline.activity",
    };
  }
  return {
    icon: item.status === "completed" ? CheckCircle2 : Clock,
    dotClass: statusDotClass(item.status),
    labelKey: "detail.timeline.runStatus",
  };
}

function statusDotClass(status: RunTimelineItem["status"]) {
  switch (status) {
    case "completed":
      return "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "cancelled":
      return "bg-muted text-muted-foreground";
    case "running":
    case "started":
      return "bg-primary/10 text-primary";
    default:
      return "bg-muted text-muted-foreground";
  }
}

export type { RunTimelineItemType };
