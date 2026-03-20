import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Pagination } from "@/components/shared/pagination";
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

  // Poll while running to detect completion
  useEffect(() => {
    if (!isRunning) return;
    const interval = setInterval(onRefresh, 3000);
    return () => clearInterval(interval);
  }, [isRunning, onRefresh]);

  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <h4 className="font-medium">{t("detail.runHistory")}</h4>
        <Button variant="ghost" size="sm" onClick={() => loadRunLog()} className="text-xs">
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
              <div key={i} className="rounded-md border p-3 text-sm">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="text-muted-foreground">{formatDate(new Date(entry.ts))}</span>
                    {entry.durationMs != null && entry.durationMs > 0 && (
                      <span className="text-xs text-muted-foreground">({formatDuration(entry.durationMs)})</span>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    {entry.inputTokens != null && entry.inputTokens > 0 && (
                      <span className="text-xs text-muted-foreground">
                        {t("detail.inOut", {
                          input: formatTokens(entry.inputTokens),
                          output: formatTokens(entry.outputTokens ?? 0),
                        })}
                      </span>
                    )}
                    <Badge
                      variant={
                        entry.status === "ok" || entry.status === "success" ? "success" : "destructive"
                      }
                    >
                      {entry.status || "unknown"}
                    </Badge>
                  </div>
                </div>
                {entry.summary && (
                  <p className="mt-1 line-clamp-3 text-muted-foreground">{entry.summary}</p>
                )}
                {entry.error && (
                  <p className="mt-1 text-destructive">{entry.error}</p>
                )}
              </div>
            ))}
          </div>

          <Pagination
            page={runLogPage}
            pageSize={runLogPageSize}
            total={runLogTotal}
            totalPages={runLogTotalPages}
            onPageChange={(p) => { setRunLogPage(p); loadRunLog(p); }}
            onPageSizeChange={(s) => { setRunLogPageSize(s); setRunLogPage(1); loadRunLog(1, s); }}
            pageSizes={[10, 20, 50]}
          />
        </>
      )}
    </div>
  );
}
