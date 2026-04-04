import { useState, useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatRelativeTime } from "@/lib/format";
import type { TeamTaskEvent } from "@/types/team";

const LIMIT = 50;

const EVENT_BADGE_CLASSES: Record<string, string> = {
  created:    "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  updated:    "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  claimed:    "bg-indigo-100 text-indigo-700 dark:bg-indigo-900 dark:text-indigo-300",
  assigned:   "bg-indigo-100 text-indigo-700 dark:bg-indigo-900 dark:text-indigo-300",
  dispatched: "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300",
  completed:  "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
  approved:   "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
  failed:     "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
  rejected:   "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
  cancelled:  "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400",
  commented:  "bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300",
  progress:   "bg-cyan-100 text-cyan-700 dark:bg-cyan-900 dark:text-cyan-300",
  reviewed:   "bg-orange-100 text-orange-700 dark:bg-orange-900 dark:text-orange-300",
  stale:      "bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300",
};

interface EventDataDetails {
  reason?: string;
  comment_text?: string;
  progress_percent?: number;
  progress_step?: string;
  [key: string]: unknown;
}

function EventDataRow({ data }: { data: Record<string, unknown> }) {
  const [expanded, setExpanded] = useState(false);
  const d = data as EventDataDetails;
  const summary = d.reason || d.comment_text
    || (d.progress_percent != null ? `${d.progress_percent}%${d.progress_step ? ` — ${d.progress_step}` : ""}` : null);

  if (!summary && Object.keys(data).length === 0) return null;

  return (
    <div className="mt-0.5">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="text-[10px] text-muted-foreground underline-offset-2 hover:underline"
      >
        {expanded ? "hide" : "details"}
      </button>
      {expanded && (
        <pre className="mt-1 max-h-20 overflow-auto rounded bg-muted px-2 py-1 text-[10px] text-muted-foreground">
          {JSON.stringify(data, null, 2)}
        </pre>
      )}
      {!expanded && summary && (
        <span className="ml-1 text-[10px] text-muted-foreground">{summary}</span>
      )}
    </div>
  );
}

interface TeamAuditLogsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  teamId: string;
}

export function TeamAuditLogsModal({ open, onOpenChange, teamId }: TeamAuditLogsModalProps) {
  const { t } = useTranslation("teams");
  const ws = useWs();

  const [events, setEvents] = useState<TeamTaskEvent[]>([]);
  const [loading, setLoading] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchEvents = useCallback(async (offset: number, append: boolean) => {
    setLoading(true);
    setError(null);
    try {
      const res = await ws.call<{ events: TeamTaskEvent[]; count: number }>(
        Methods.TEAMS_EVENTS_LIST,
        { team_id: teamId, limit: LIMIT, offset },
      );
      const incoming = res.events ?? [];
      setEvents((prev) => append ? [...prev, ...incoming] : incoming);
      setHasMore(incoming.length === LIMIT);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load events");
    } finally {
      setLoading(false);
    }
  }, [ws, teamId]);

  // Load on open
  useEffect(() => {
    if (open) {
      setEvents([]);
      fetchEvents(0, false);
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleLoadMore = useCallback(() => {
    fetchEvents(events.length, true);
  }, [events.length, fetchEvents]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:max-w-2xl max-h-[90dvh] flex flex-col p-0">
        <DialogHeader className="px-4 pt-4 pb-2">
          <DialogTitle>{t("auditLogs.title")}</DialogTitle>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto overscroll-contain px-4 pb-4">
          {error && (
            <p className="py-8 text-center text-sm text-destructive">{error}</p>
          )}
          {events.length === 0 && !loading && !error && (
            <p className="py-8 text-center text-sm text-muted-foreground">{t("auditLogs.empty")}</p>
          )}

          <div className="space-y-0">
            {events.map((e, i) => (
              <div key={e.id} className="relative flex gap-3 pb-3 last:pb-0">
                {/* Dot + vertical line */}
                <div className="flex flex-col items-center pt-1.5">
                  <div className="h-2 w-2 shrink-0 rounded-full bg-primary" />
                  {i < events.length - 1 && <div className="mt-0.5 flex-1 w-px bg-border" />}
                </div>

                {/* Content */}
                <div className="flex-1 min-w-0 pb-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge
                      variant="outline"
                      className={`text-[10px] border-0 font-medium ${EVENT_BADGE_CLASSES[e.event_type] ?? "bg-muted text-muted-foreground"}`}
                    >
                      {t(`auditLogs.event.${e.event_type}`, { defaultValue: e.event_type })}
                    </Badge>
                    <span className="text-xs text-muted-foreground truncate max-w-[120px]">
                      {e.actor_type === "human" ? "Human" : (e.actor_id?.slice(0, 8) ?? "—")}
                    </span>
                    <span className="text-[11px] text-muted-foreground ml-auto shrink-0">
                      {formatRelativeTime(e.created_at)}
                    </span>
                  </div>
                  {e.task_id && (
                    <div className="text-[10px] text-muted-foreground mt-0.5">
                      task: {e.task_id.slice(0, 8)}
                    </div>
                  )}
                  {e.data && Object.keys(e.data).length > 0 && (
                    <EventDataRow data={e.data} />
                  )}
                </div>
              </div>
            ))}
          </div>

          {loading && (
            <p className="py-4 text-center text-xs text-muted-foreground">Loading...</p>
          )}

          {hasMore && !loading && (
            <div className="pt-2 text-center">
              <Button variant="outline" size="sm" onClick={handleLoadMore}>
                {t("auditLogs.loadMore")}
              </Button>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
