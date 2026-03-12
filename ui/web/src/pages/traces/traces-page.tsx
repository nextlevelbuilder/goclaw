import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Activity, GitFork, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { formatDate, formatDuration, formatTokens, computeDurationMs } from "@/lib/format";
import { useTraces, type TraceData } from "./hooks/use-traces";
import { TraceDetailDialog } from "./trace-detail-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";

export function TracesPage() {
  const { t } = useTranslation("traces");
  const { t: tc } = useTranslation("common");
  const [agentFilter, setAgentFilter] = useState<string>();
  const [channelFilter, setChannelFilter] = useState<string>();
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const { agents } = useAgents();
  const { instances: channels } = useChannelInstances();

  const { traces, total, loading, fetching, refresh, getTrace } = useTraces({
    agentId: agentFilter,
    channel: channelFilter,
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });
  const spinning = useMinLoading(fetching);
  const showSkeleton = useDeferredLoading(loading && traces.length === 0);

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
          </Button>
        }
      />

      <div className="mt-4 flex flex-wrap items-center gap-2">
        {/* Agent filter */}
        <Select
          value={agentFilter ?? "__all__"}
          onValueChange={(v) => { setAgentFilter(v === "__all__" ? undefined : v); setPage(1); }}
        >
          <SelectTrigger className="h-8 w-44 text-xs">
            <SelectValue placeholder={t("allAgents")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{t("allAgents")}</SelectItem>
            {agents.map((a) => (
              <SelectItem key={a.id} value={a.id}>{a.display_name || a.agent_key || a.id}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Channel filter */}
        <Select
          value={channelFilter ?? "__all__"}
          onValueChange={(v) => { setChannelFilter(v === "__all__" ? undefined : v); setPage(1); }}
        >
          <SelectTrigger className="h-8 w-44 text-xs">
            <SelectValue placeholder={t("allChannels")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{t("allChannels")}</SelectItem>
            {channels.map((ch) => (
              <SelectItem key={ch.id} value={ch.name}>{ch.display_name || ch.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={8} />
        ) : traces.length === 0 ? (
          <EmptyState
            icon={Activity}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[700px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.duration")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.tokens")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.spans")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.time")}</th>
                </tr>
              </thead>
              <tbody>
                {traces.map((trace: TraceData) => (
                  <tr
                    key={trace.id}
                    className="cursor-pointer border-b last:border-0 hover:bg-muted/30"
                    onClick={() => setSelectedTraceId(trace.id)}
                  >
                    <td className="max-w-[200px] truncate px-4 py-3 font-medium">
                      {trace.parent_trace_id && (
                        <GitFork className="mr-1.5 inline-block h-3.5 w-3.5 text-muted-foreground" />
                      )}
                      {trace.name || t("unnamed")}
                      {trace.channel && (
                        <Badge variant="outline" className="ml-2 text-xs">
                          {trace.channel}
                        </Badge>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <StatusBadge status={trace.status} />
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {formatDuration(trace.duration_ms || computeDurationMs(trace.start_time, trace.end_time))}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      <div>{formatTokens(trace.total_input_tokens)} / {formatTokens(trace.total_output_tokens)}</div>
                      {(trace.metadata?.total_cache_read_tokens ?? 0) > 0 && (
                        <div className="text-xs text-green-400">
                          {formatTokens(trace.metadata!.total_cache_read_tokens!)} {t("cached")}
                        </div>
                      )}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {trace.span_count}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {formatDate(trace.start_time)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <Pagination
              page={page}
              pageSize={pageSize}
              total={total}
              totalPages={totalPages}
              onPageChange={setPage}
              onPageSizeChange={(size) => { setPageSize(size); setPage(1); }}
            />
          </div>
        )}
      </div>

      {selectedTraceId && (
        <TraceDetailDialog
          traceId={selectedTraceId}
          onClose={() => setSelectedTraceId(null)}
          getTrace={getTrace}
          onNavigateTrace={setSelectedTraceId}
        />
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const variant =
    status === "ok" || status === "success" || status === "completed"
      ? "success"
      : status === "error" || status === "failed"
        ? "destructive"
        : status === "running" || status === "pending"
          ? "info"
          : "secondary";

  return <Badge variant={variant} className="text-xs">{status || "unknown"}</Badge>;
}
