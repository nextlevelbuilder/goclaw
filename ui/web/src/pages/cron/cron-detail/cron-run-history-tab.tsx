import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, Clock, AlertTriangle, ChevronDown, ChevronUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Pagination } from "@/components/shared/pagination";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { formatDate, formatTokens } from "@/lib/format";
import type { CronJob, CronRunLogEntry } from "../hooks/use-cron";

interface CronRunHistoryTabProps {
  job: CronJob;
  getRunLog: (id: string, limit?: number, offset?: number) => Promise<{ entries: CronRunLogEntry[]; total: number }>;
  onRefresh: () => void;
}

function formatDuration(ms?: number): string {
  if (!ms) return "-";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function RunEntry({ entry }: { entry: CronRunLogEntry }) {
  const { t } = useTranslation("cron");
  const [expanded, setExpanded] = useState(false);
  const isSuccess = entry.status === "ok" || entry.status === "success";
  const hasDetails = !!(entry.summary || entry.error);

  return (
    <div className="rounded-lg border bg-card overflow-hidden">
      {/* Header row — always visible */}
      <button
        type="button"
        className="flex w-full items-center gap-3 px-3 py-2.5 text-left hover:bg-muted/30 transition-colors sm:px-4"
        onClick={() => hasDetails && setExpanded(!expanded)}
        disabled={!hasDetails}
      >
        {/* Status dot */}
        <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${isSuccess ? "bg-emerald-500" : "bg-destructive"}`} />

        {/* Timestamp + duration */}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-sm">
            <span className="font-medium">{formatDate(new Date(entry.ts))}</span>
            {entry.durationMs != null && entry.durationMs > 0 && (
              <span className="text-xs text-muted-foreground">({formatDuration(entry.durationMs)})</span>
            )}
          </div>
          {/* Summary preview (collapsed) */}
          {!expanded && entry.summary && (
            <p className="mt-0.5 truncate text-xs text-muted-foreground">{entry.summary}</p>
          )}
        </div>

        {/* Tokens */}
        {entry.inputTokens != null && entry.inputTokens > 0 && (
          <span className="hidden shrink-0 text-xs text-muted-foreground sm:block">
            {t("detail.inOut", {
              input: formatTokens(entry.inputTokens),
              output: formatTokens(entry.outputTokens ?? 0),
            })}
          </span>
        )}

        {/* Status badge */}
        <Badge variant={isSuccess ? "success" : "destructive"} className="shrink-0">
          {entry.status || "unknown"}
        </Badge>

        {/* Expand chevron */}
        {hasDetails && (
          <span className="shrink-0 text-muted-foreground">
            {expanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
          </span>
        )}
      </button>

      {/* Expanded details */}
      {expanded && (
        <div className="border-t px-3 py-3 sm:px-4">
          {entry.summary && (
            <div className="rounded-md bg-muted/30 p-3">
              <MarkdownRenderer content={entry.summary} className="prose-sm max-w-none" />
            </div>
          )}
          {entry.error && (
            <div className="mt-2 rounded-md border border-destructive/20 bg-destructive/5 p-3">
              <div className="mb-1 flex items-center gap-1.5">
                <AlertTriangle className="h-3.5 w-3.5 text-destructive" />
                <span className="text-xs font-medium text-destructive">{t("detail.lastError")}</span>
              </div>
              <MarkdownRenderer content={entry.error} className="prose-sm max-w-none text-destructive/80" />
            </div>
          )}
          {/* Token details on mobile */}
          {entry.inputTokens != null && entry.inputTokens > 0 && (
            <div className="mt-2 text-xs text-muted-foreground sm:hidden">
              {t("detail.inOut", {
                input: formatTokens(entry.inputTokens),
                output: formatTokens(entry.outputTokens ?? 0),
              })}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function CronRunHistoryTab({ job, getRunLog, onRefresh }: CronRunHistoryTabProps) {
  const { t } = useTranslation("cron");
  const [runLog, setRunLog] = useState<CronRunLogEntry[]>([]);
  const [runLogTotal, setRunLogTotal] = useState(0);
  const [runLogLoading, setRunLogLoading] = useState(true);
  const [runLogPage, setRunLogPage] = useState(1);
  const [runLogPageSize, setRunLogPageSize] = useState(10);

  const isRunning = job.state?.lastStatus === "running";

  const loadRunLog = useCallback(async (page?: number, pageSize?: number) => {
    const p = page ?? runLogPage;
    const ps = pageSize ?? runLogPageSize;
    setRunLogLoading(true);
    try {
      const { entries, total } = await getRunLog(job.id, ps, (p - 1) * ps);
      setRunLog(entries);
      setRunLogTotal(total);
    } finally {
      setRunLogLoading(false);
    }
  }, [job.id, getRunLog, runLogPage, runLogPageSize]);

  const runLogTotalPages = Math.ceil(runLogTotal / runLogPageSize);

  useEffect(() => {
    loadRunLog();
  }, [loadRunLog]);

  // Poll while running
  useEffect(() => {
    if (!isRunning) return;
    const interval = setInterval(onRefresh, 3000);
    return () => clearInterval(interval);
  }, [isRunning, onRefresh]);

  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Clock className="h-4 w-4 text-muted-foreground" />
          <h4 className="font-medium">{t("detail.runHistory")}</h4>
          {runLogTotal > 0 && (
            <span className="text-xs text-muted-foreground">({runLogTotal})</span>
          )}
        </div>
        <Button variant="ghost" size="sm" onClick={() => loadRunLog()} className="gap-1 text-xs">
          <RefreshCw className="h-3 w-3" />
          {t("detail.refresh")}
        </Button>
      </div>

      {runLogLoading && runLog.length === 0 ? (
        <div className="flex items-center justify-center py-8">
          <div className="h-5 w-5 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
        </div>
      ) : runLog.length === 0 ? (
        <p className="py-8 text-center text-sm text-muted-foreground">{t("detail.noHistory")}</p>
      ) : (
        <>
          <div className="space-y-2">
            {runLog.map((entry, i) => (
              <RunEntry key={`${entry.ts}-${i}`} entry={entry} />
            ))}
          </div>

          <div className="mt-4">
            <Pagination
              page={runLogPage}
              pageSize={runLogPageSize}
              total={runLogTotal}
              totalPages={runLogTotalPages}
              onPageChange={(p) => { setRunLogPage(p); loadRunLog(p); }}
              onPageSizeChange={(s) => { setRunLogPageSize(s); setRunLogPage(1); loadRunLog(1, s); }}
              pageSizes={[10, 20, 50]}
            />
          </div>
        </>
      )}
    </div>
  );
}
