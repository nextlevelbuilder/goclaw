import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, Clock, AlertTriangle, CheckCircle2, XCircle, ChevronDown, Zap } from "lucide-react";
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

function StatusIcon({ status }: { status?: string }) {
  const isSuccess = status === "ok" || status === "success";
  if (isSuccess) return <CheckCircle2 className="h-4 w-4 text-emerald-500 shrink-0" />;
  return <XCircle className="h-4 w-4 text-destructive shrink-0" />;
}

function RunEntry({ entry }: { entry: CronRunLogEntry }) {
  const { t } = useTranslation("cron");
  const [expanded, setExpanded] = useState(false);
  const isSuccess = entry.status === "ok" || entry.status === "success";
  const hasDetails = !!(entry.summary || entry.error);

  return (
    <div className={`group border-b last:border-b-0 ${expanded ? "bg-muted/20" : "hover:bg-muted/10"} transition-colors`}>
      {/* Row */}
      <button
        type="button"
        className="flex w-full items-center gap-3 px-3 py-2.5 text-left"
        onClick={() => hasDetails && setExpanded(!expanded)}
        disabled={!hasDetails}
      >
        <StatusIcon status={entry.status} />

        {/* Time */}
        <span className="text-sm tabular-nums shrink-0 w-[140px] sm:w-auto">
          {formatDate(new Date(entry.ts))}
        </span>

        {/* Duration */}
        {entry.durationMs != null && entry.durationMs > 0 && (
          <Badge variant="outline" className="hidden sm:inline-flex gap-1 text-2xs font-normal shrink-0">
            <Zap className="h-2.5 w-2.5" />
            {formatDuration(entry.durationMs)}
          </Badge>
        )}

        {/* Summary preview */}
        <span className="flex-1 min-w-0 truncate text-xs text-muted-foreground">
          {entry.error ? entry.error.split("\n")[0] : entry.summary?.split("\n")[0] || ""}
        </span>

        {/* Tokens */}
        {entry.inputTokens != null && entry.inputTokens > 0 && (
          <span className="hidden sm:block shrink-0 text-2xs text-muted-foreground tabular-nums">
            {formatTokens(entry.inputTokens)}/{formatTokens(entry.outputTokens ?? 0)}
          </span>
        )}

        {/* Status */}
        <Badge
          variant={isSuccess ? "success" : "destructive"}
          className="shrink-0 text-2xs min-w-[36px] justify-center"
        >
          {entry.status || "?"}
        </Badge>

        {/* Expand */}
        {hasDetails && (
          <ChevronDown className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${expanded ? "rotate-180" : ""}`} />
        )}
      </button>

      {/* Expanded */}
      {expanded && (
        <div className="px-3 pb-3 pl-10">
          {entry.summary && (
            <div className="rounded-md bg-background border p-3 text-sm">
              <MarkdownRenderer content={entry.summary} className="prose-sm max-w-none" />
            </div>
          )}
          {entry.error && (
            <div className="mt-2 rounded-md border border-destructive/20 bg-destructive/5 p-3">
              <div className="mb-1.5 flex items-center gap-1.5">
                <AlertTriangle className="h-3 w-3 text-destructive" />
                <span className="text-2xs font-semibold uppercase tracking-wider text-destructive">{t("detail.lastError")}</span>
              </div>
              <pre className="text-xs text-destructive/80 whitespace-pre-wrap break-all font-mono">{entry.error}</pre>
            </div>
          )}
          {/* Mobile: duration + tokens */}
          <div className="mt-2 flex items-center gap-3 text-2xs text-muted-foreground sm:hidden">
            {entry.durationMs != null && entry.durationMs > 0 && (
              <span>{formatDuration(entry.durationMs)}</span>
            )}
            {entry.inputTokens != null && entry.inputTokens > 0 && (
              <span>{formatTokens(entry.inputTokens)} in / {formatTokens(entry.outputTokens ?? 0)} out</span>
            )}
          </div>
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
          <h4 className="text-sm font-semibold">{t("detail.runHistory")}</h4>
          {runLogTotal > 0 && (
            <Badge variant="secondary" className="text-2xs">{runLogTotal}</Badge>
          )}
        </div>
        <Button variant="ghost" size="sm" onClick={() => loadRunLog()} className="gap-1.5 text-xs h-7">
          <RefreshCw className={`h-3 w-3 ${runLogLoading ? "animate-spin" : ""}`} />
          {t("detail.refresh")}
        </Button>
      </div>

      {runLogLoading && runLog.length === 0 ? (
        <div className="flex items-center justify-center py-12">
          <div className="h-5 w-5 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
        </div>
      ) : runLog.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
          <Clock className="h-8 w-8 mb-2 opacity-30" />
          <p className="text-sm">{t("detail.noHistory")}</p>
        </div>
      ) : (
        <>
          <div className="rounded-lg border overflow-hidden bg-card">
            {runLog.map((entry, i) => (
              <RunEntry key={`${entry.ts}-${i}`} entry={entry} />
            ))}
          </div>

          {runLogTotalPages > 1 && (
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
          )}
        </>
      )}
    </div>
  );
}
