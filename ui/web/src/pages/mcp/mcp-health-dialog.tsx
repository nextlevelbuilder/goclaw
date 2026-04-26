import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { StatusBadge } from "@/components/shared/status-badge";
import { Pagination } from "@/components/shared/pagination";
import { formatDate, formatRelativeTime } from "@/lib/format";
import type { MCPServerData, MCPHealthCheck } from "@/types/mcp";

interface MCPHealthDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server: MCPServerData;
  onLoadChecks: (serverId: string, limit: number, offset: number) => Promise<{ checks: MCPHealthCheck[]; total: number }>;
}

const statusVariant: Record<string, "success" | "warning" | "error"> = {
  healthy: "success",
  unhealthy: "error",
  reconnecting: "warning",
};

const PAGE_SIZE = 20;

export function MCPHealthDialog({ open, onOpenChange, server, onLoadChecks }: MCPHealthDialogProps) {
  const { t } = useTranslation("mcp");
  const [checks, setChecks] = useState<MCPHealthCheck[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const loadChecks = useCallback(async (p: number) => {
    setLoading(true);
    setError("");
    try {
      const offset = (p - 1) * PAGE_SIZE;
      const res = await onLoadChecks(server.id, PAGE_SIZE, offset);
      setChecks(res.checks);
      setTotal(res.total);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load health checks");
    } finally {
      setLoading(false);
    }
  }, [server.id, onLoadChecks]);

  useEffect(() => {
    if (open) {
      setPage(1);
      loadChecks(1);
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const handlePageChange = (p: number) => {
    setPage(p);
    loadChecks(p);
  };

  const healthyCount = checks.filter((c) => c.status === "healthy").length;
  const avgLatency = checks.filter((c) => c.latency_ms != null).reduce((sum, c) => sum + (c.latency_ms ?? 0), 0) / (healthyCount || 1);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[80vh] flex flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {t("health.title", { name: server.display_name || server.name })}
            {server.health_status && (() => {
              const hb = server.health_status.connected
                ? { status: "success" as const, label: t("status.connected") }
                : server.health_status.reconnect_attempts > 0
                  ? { status: "warning" as const, label: t("status.reconnecting") }
                  : { status: "error" as const, label: t("status.disconnected") };
              return <StatusBadge status={hb.status} label={hb.label} />;
            })()}
          </DialogTitle>
        </DialogHeader>

        {total > 0 && (
          <div className="flex gap-4 text-sm text-muted-foreground">
            <span>{t("health.summary.total")}: <strong className="text-foreground">{total}</strong></span>
            {healthyCount > 0 && (
              <span>{t("health.summary.avgLatency")}: <strong className="text-foreground">{Math.round(avgLatency)}ms</strong></span>
            )}
          </div>
        )}

        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 flex-1">
          {loading && checks.length === 0 ? (
            <div className="flex items-center justify-center py-8">
              <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
            </div>
          ) : error ? (
            <p className="py-8 text-center text-sm text-destructive">{error}</p>
          ) : checks.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {t("health.noHistory")}
            </p>
          ) : (
            <div className="space-y-1.5">
              {checks.map((check) => (
                <div key={check.id} className="flex items-center gap-3 rounded-md border px-3 py-2 text-sm">
                  <StatusBadge
                    status={statusVariant[check.status] ?? "default"}
                    label={t(`health.status.${check.status}`)}
                  />
                  <span className="text-muted-foreground shrink-0" title={formatDate(new Date(check.checked_at))}>
                    {formatRelativeTime(check.checked_at)}
                  </span>
                  {check.latency_ms != null && (
                    <span className="text-muted-foreground">{t("health.latency", { ms: check.latency_ms })}</span>
                  )}
                  {check.health_failures > 0 && (
                    <span className="text-warning text-xs">{t("health.failures", { count: check.health_failures })}</span>
                  )}
                  {check.error && (
                    <span className="text-destructive text-xs truncate" title={check.error}>{check.error}</span>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        {total > PAGE_SIZE && (
          <Pagination
            page={page}
            pageSize={PAGE_SIZE}
            total={total}
            totalPages={totalPages}
            onPageChange={handlePageChange}
            onPageSizeChange={() => {}}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
