import { useState, useEffect } from "react";
import { BarChart3, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { formatTokens } from "@/lib/format";
import { useUsage } from "./hooks/use-usage";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";

export function UsagePage() {
  const { records, total, summary, loading, loadRecords, loadSummary } = useUsage();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && records.length === 0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  useEffect(() => {
    loadRecords({ limit: pageSize, offset: (page - 1) * pageSize });
  }, [page, pageSize]);

  const handleRefresh = () => {
    loadRecords({ limit: pageSize, offset: (page - 1) * pageSize });
    loadSummary();
  };

  const agentEntries = summary?.byAgent
    ? Object.entries(summary.byAgent).sort(
        ([, a], [, b]) => b.totalTokens - a.totalTokens,
      )
    : [];

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Usage"
        description="Token usage and costs by agent"
        actions={
          <Button variant="outline" size="sm" onClick={handleRefresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> Refresh
          </Button>
        }
      />

      {showSkeleton ? (
        <div className="mt-6">
          <TableSkeleton rows={4} />
        </div>
      ) : (
        <>
          {/* Summary cards */}
          {summary && agentEntries.length > 0 && (
            <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {agentEntries.map(([agentId, data]) => (
                <div key={agentId} className="rounded-lg border p-4">
                  <div className="flex items-center justify-between">
                    <h4 className="text-sm font-medium">{agentId}</h4>
                    <Badge variant="secondary">{data.sessions} sessions</Badge>
                  </div>
                  <div className="mt-3 space-y-1 text-sm text-muted-foreground">
                    <div className="flex justify-between">
                      <span>Input tokens</span>
                      <span className="font-medium text-foreground">{formatTokens(data.inputTokens)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span>Output tokens</span>
                      <span className="font-medium text-foreground">{formatTokens(data.outputTokens)}</span>
                    </div>
                    <div className="flex justify-between border-t pt-1">
                      <span>Total</span>
                      <span className="font-medium text-foreground">{formatTokens(data.totalTokens)}</span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Recent records table */}
          <div className="mt-6">
            <h3 className="mb-3 text-sm font-medium">Recent Records</h3>
            {records.length === 0 ? (
              <EmptyState
                icon={BarChart3}
                title="No usage data"
                description="Usage data will appear here after agents process requests."
              />
            ) : (
              <div className="rounded-md border">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b bg-muted/50">
                      <th className="px-4 py-3 text-left font-medium">Agent</th>
                      <th className="px-4 py-3 text-left font-medium">Model</th>
                      <th className="px-4 py-3 text-left font-medium">Provider</th>
                      <th className="px-4 py-3 text-right font-medium">Input</th>
                      <th className="px-4 py-3 text-right font-medium">Output</th>
                      <th className="px-4 py-3 text-right font-medium">Total</th>
                    </tr>
                  </thead>
                  <tbody>
                    {records.map((r, i) => (
                      <tr key={i} className="border-b last:border-0 hover:bg-muted/30">
                        <td className="px-4 py-3 font-medium">{r.agentId}</td>
                        <td className="px-4 py-3">
                          <Badge variant="outline">{r.model}</Badge>
                        </td>
                        <td className="px-4 py-3 text-muted-foreground">{r.provider}</td>
                        <td className="px-4 py-3 text-right text-muted-foreground">
                          {formatTokens(r.inputTokens)}
                        </td>
                        <td className="px-4 py-3 text-right text-muted-foreground">
                          {formatTokens(r.outputTokens)}
                        </td>
                        <td className="px-4 py-3 text-right font-medium">
                          {formatTokens(r.totalTokens)}
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
        </>
      )}
    </div>
  );
}
