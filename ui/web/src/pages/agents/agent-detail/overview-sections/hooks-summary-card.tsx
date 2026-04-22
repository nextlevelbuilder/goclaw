import { useState, useMemo } from "react";
import { Webhook, ChevronDown, ChevronUp, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { useHooksList } from "@/hooks/use-hooks";

interface HooksSummaryCardProps {
  agentId: string;
  onViewAll: () => void;
  onAddHook: () => void;
}

const EVENT_COLORS: Record<string, string> = {
  session_start: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300",
  user_prompt_submit: "bg-violet-100 text-violet-700 dark:bg-violet-900/30 dark:text-violet-300",
  pre_tool_use: "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300",
  post_tool_use: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300",
  stop: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300",
  subagent_start: "bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300",
  subagent_stop: "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-300",
};

export function HooksSummaryCard({ agentId, onViewAll, onAddHook }: HooksSummaryCardProps) {
  const { t } = useTranslation("agents");
  const [expanded, setExpanded] = useState(false);
  const { data: hooks = [], isPending } = useHooksList({ agentId, scope: "agent" });

  const groupedByEvent = useMemo(() => {
    const groups: Record<string, number> = {};
    hooks.forEach((h) => {
      groups[h.event] = (groups[h.event] || 0) + 1;
    });
    return Object.entries(groups)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 5);
  }, [hooks]);

  const totalCount = hooks.length;

  // Loading state
  if (isPending) {
    return (
      <div className="rounded-lg border p-4">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
          {t("hooks.card.loading")}
        </div>
      </div>
    );
  }

  // Empty state
  if (totalCount === 0) {
    return (
      <div className="rounded-lg border p-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Webhook className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm font-medium">{t("hooks.card.title")}</span>
          </div>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">{t("hooks.card.empty")}</p>
        <p className="mt-1 text-xs text-muted-foreground">{t("hooks.card.emptyDesc")}</p>
        <Button size="sm" variant="outline" onClick={onAddHook} className="mt-3 gap-1">
          <Plus className="h-3.5 w-3.5" />
          {t("hooks.card.addHook")}
        </Button>
      </div>
    );
  }

  // Has hooks - collapsible
  return (
    <div className="rounded-lg border p-4 space-y-3">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between gap-3"
      >
        <div className="flex items-center gap-2">
          <Webhook className="h-4 w-4 text-primary" />
          <span className="text-sm font-medium">{t("hooks.card.title")}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <Badge variant="secondary" className="text-xs">
            {totalCount}
          </Badge>
          {expanded ? (
            <ChevronUp className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          )}
        </div>
      </button>

      {expanded && (
        <>
          <div className="space-y-1.5 pt-1">
            {groupedByEvent.map(([event, count]) => (
              <div key={event} className="flex items-center justify-between text-xs">
                <span
                  className={`inline-flex items-center rounded px-1.5 py-0.5 font-medium ${EVENT_COLORS[event] ?? "bg-muted text-muted-foreground"}`}
                >
                  {event}
                </span>
                <span className="text-muted-foreground">
                  {t("hooks.card.nHooks", { count })}
                </span>
              </div>
            ))}
          </div>

          <Button
            variant="link"
            size="sm"
            onClick={onViewAll}
            className="h-auto p-0 text-xs"
          >
            {t("hooks.card.viewAll", { count: totalCount })} &rarr;
          </Button>
        </>
      )}
    </div>
  );
}
