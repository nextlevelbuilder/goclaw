import { useState, useEffect, useCallback } from "react";
import { RefreshCw, ChevronLeft, ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { useMinLoading } from "@/hooks/use-min-loading";
import type { HeartbeatLog } from "@/pages/agents/hooks/use-agent-heartbeat";
import { formatRelativeTime, formatDate, formatDuration } from "@/lib/format";

const PAGE_SIZE = 20;

type LogStatus = "ok" | "error" | "suppressed" | "skipped";

function statusVariant(status: string): "success" | "destructive" | "secondary" | "outline" {
  switch (status as LogStatus) {
    case "ok": return "success";
    case "error": return "destructive";
    case "suppressed": return "secondary";
    default: return "outline";
  }
}

interface HeartbeatLogsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  fetchLogs: (limit: number, offset: number) => Promise<{ logs: HeartbeatLog[]; total: number }>;
}

export function HeartbeatLogsDialog({
  open, onOpenChange, fetchLogs,
}: HeartbeatLogsDialogProps) {
  const { t } = useTranslation("agents");

  const [logs, setLogs] = useState<HeartbeatLog[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [fetching, setFetching] = useState(false);
  const showSpin = useMinLoading(fetching, 600);

  const load = useCallback(
    async (newOffset: number) => {
      setFetching(true);
      try {
        const res = await fetchLogs(PAGE_SIZE, newOffset);
        setLogs(res.logs);
        setTotal(res.total);
        setOffset(newOffset);
      } catch {
        // toast handled inside hook if needed
      } finally {
        setFetching(false);
      }
    },
    [fetchLogs],
  );

  useEffect(() => {
    if (open) {
      setOffset(0);
      load(0);
    }
  }, [open, load]);

  const hasPrev = offset > 0;
  const hasNext = offset + PAGE_SIZE < total;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-3xl">
        <DialogHeader className="flex-row items-center justify-between gap-2 pr-8">
          <DialogTitle>{t("heartbeat.logsTitle")}</DialogTitle>
          <Button
            variant="outline"
            size="sm"
            onClick={() => load(offset)}
            disabled={showSpin}
            className="shrink-0"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${showSpin ? "animate-spin" : ""}`} />
            {t("heartbeat.refresh")}
          </Button>
        </DialogHeader>

        <div className="overflow-y-auto min-h-0 max-h-[60vh] -mx-4 px-4 sm:-mx-6 sm:px-6 overscroll-contain">
          {fetching && logs.length === 0 ? (
            <div className="flex items-center justify-center py-12">
              <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
            </div>
          ) : logs.length === 0 ? (
            <p className="py-12 text-center text-sm text-muted-foreground">
              {t("heartbeat.noLogs")}
            </p>
          ) : (
            <div className="space-y-2 py-1">
              {logs.map((log) => (
                <div key={log.id} className="rounded-md border p-3 text-sm space-y-1">
                  <div className="flex items-center justify-between gap-2 flex-wrap">
                    <span className="text-xs text-muted-foreground" title={formatDate(log.ranAt)}>
                      {formatRelativeTime(log.ranAt)}
                    </span>
                    <div className="flex items-center gap-1.5">
                      {(log.inputTokens != null || log.outputTokens != null) && (
                        <span className="text-2xs text-muted-foreground">
                          {log.inputTokens ?? 0}↓ {log.outputTokens ?? 0}↑
                        </span>
                      )}
                      {log.durationMs != null && (
                        <span className="text-2xs text-muted-foreground">
                          {formatDuration(log.durationMs)}
                        </span>
                      )}
                      <Badge variant={statusVariant(log.status)} className="text-2xs">
                        {log.status}
                      </Badge>
                    </div>
                  </div>
                  {log.summary && (
                    <p className="text-xs text-muted-foreground line-clamp-2">{log.summary}</p>
                  )}
                  {log.skipReason && (
                    <p className="text-xs text-muted-foreground italic">{log.skipReason}</p>
                  )}
                  {log.error && (
                    <p className="text-xs text-destructive">{log.error}</p>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Pagination */}
        <div className="flex items-center justify-between gap-2 border-t pt-3 text-xs text-muted-foreground">
          <span>
            {total > 0
              ? t("heartbeat.logsPagination", {
                  from: offset + 1,
                  to: Math.min(offset + PAGE_SIZE, total),
                  total,
                })
              : ""}
          </span>
          <div className="flex gap-1">
            <Button
              variant="outline"
              size="sm"
              className="h-7 w-7 p-0"
              onClick={() => load(offset - PAGE_SIZE)}
              disabled={!hasPrev || fetching}
            >
              <ChevronLeft className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="h-7 w-7 p-0"
              onClick={() => load(offset + PAGE_SIZE)}
              disabled={!hasNext || fetching}
            >
              <ChevronRight className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
