import { useState } from "react";
import { Activity, GitFork, RefreshCw, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { formatDate, formatDuration, formatTokens, computeDurationMs } from "@/lib/format";
import { useTraces, type TraceData } from "./hooks/use-traces";
import { TraceDetailDialog } from "./trace-detail-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";

export function TracesPage() {
  const [agentFilter, setAgentFilter] = useState("");
  const [appliedAgentFilter, setAppliedAgentFilter] = useState("");
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const { traces, total, loading, fetching, refresh, getTrace } = useTraces({
    agentId: appliedAgentFilter || undefined,
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });
  const spinning = useMinLoading(fetching);
  const showSkeleton = useDeferredLoading(loading && traces.length === 0);

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  const handleFilterSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setAppliedAgentFilter(agentFilter);
    setPage(1);
  };

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Traces"
        description="LLM call traces and performance data"
        actions={
          <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> Refresh
          </Button>
        }
      />

      <form onSubmit={handleFilterSubmit} className="mt-4 flex gap-2">
        <div className="relative max-w-sm flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={agentFilter}
            onChange={(e) => setAgentFilter(e.target.value)}
            placeholder="Filter by agent ID..."
            className="pl-9"
          />
        </div>
        <Button type="submit" variant="outline" size="sm">
          Filter
        </Button>
      </form>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={8} />
        ) : traces.length === 0 ? (
          <EmptyState
            icon={Activity}
            title="No traces"
            description="No traces found. Traces are recorded when agents process requests."
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[700px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">Name</th>
                  <th className="px-4 py-3 text-left font-medium">Status</th>
                  <th className="px-4 py-3 text-left font-medium">Duration</th>
                  <th className="px-4 py-3 text-left font-medium">Tokens</th>
                  <th className="px-4 py-3 text-left font-medium">Spans</th>
                  <th className="px-4 py-3 text-left font-medium">Time</th>
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
                      {trace.name || "Unnamed"}
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
                          {formatTokens(trace.metadata!.total_cache_read_tokens!)} cached
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
