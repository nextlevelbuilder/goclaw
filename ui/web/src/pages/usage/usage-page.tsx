import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { BarChart3, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { Pagination } from "@/components/shared/pagination";
import { formatTokens, formatCost } from "@/lib/format";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useUsage } from "./hooks/use-usage";
import { useUsageAnalytics } from "./hooks/use-usage-analytics";
import { UsageFilterProvider, useUsageFilterContext } from "./context/usage-filter-context";
import { FilterBar } from "./components/filter-bar";
import { SummaryCards } from "./components/summary-cards";
import { TokenAreaChart } from "./components/token-area-chart";
import { RequestVolumeChart } from "./components/request-volume-chart";
import { DistributionRow } from "./components/distribution-row";
import { DurationChart } from "./components/duration-chart";
import { KnowledgeChart } from "./components/knowledge-chart";
import { TopModelsTable } from "./components/top-models-table";

const EMPTY_SUMMARY = { requests: 0, input_tokens: 0, output_tokens: 0, cost: 0, errors: 0, unique_users: 0, llm_calls: 0, tool_calls: 0, avg_duration_ms: 0 };

function AnalyticsDashboard() {
  const { t } = useTranslation("usage");
  const { filters, setFilter } = useUsageFilterContext();
  const { agents } = useAgents();
  const { timeseries, providerBreakdown, modelBreakdown, channelBreakdown, summary, loading, error } =
    useUsageAnalytics(filters);

  // Legacy records table state
  const { records, total, loading: recLoading, loadRecords } = useUsage();
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  useEffect(() => {
    loadRecords({ limit: pageSize, offset: (page - 1) * pageSize, agentId: filters.agentId });
  }, [page, pageSize, filters.agentId]);

  const agentList = agents.map((a) => ({
    id: a.id,
    name: (a as { display_name?: string }).display_name || a.agent_key || a.id,
  }));

  const handleExportCsv = useCallback(() => {
    const rows = [
      ["Date", "Input Tokens", "Output Tokens", "Requests", "LLM Calls", "Tool Calls", "Errors", "Cost"],
      ...timeseries.map((d) => [
        d.bucket_time,
        d.input_tokens,
        d.output_tokens,
        d.request_count,
        d.llm_call_count,
        d.tool_call_count,
        d.error_count,
        d.total_cost.toFixed(6),
      ]),
    ];
    const csv = rows.map((r) => r.join(",")).join("\n");
    const blob = new Blob([csv], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `usage-${filters.period}-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }, [timeseries, filters.period]);

  const current = summary?.current ?? EMPTY_SUMMARY;
  const previous = summary?.previous ?? EMPTY_SUMMARY;

  const apiError = error instanceof Error ? error.message : error ? String(error) : null;

  return (
    <div className="p-4 sm:p-6 space-y-4">
      <PageHeader
        title={t("analytics.title")}
        description={t("description")}
        actions={
          <Button variant="outline" size="sm" onClick={() => loadRecords()} disabled={recLoading} className="gap-1">
            <RefreshCw className={`h-3.5 w-3.5${recLoading ? " animate-spin" : ""}`} />
            {t("common:refresh", "Refresh")}
          </Button>
        }
      />

      <FilterBar
        agents={agentList}
        providerBreakdown={providerBreakdown}
        channelBreakdown={channelBreakdown}
        onExportCsv={timeseries.length > 0 ? handleExportCsv : undefined}
      />

      {apiError && (
        <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
          {t("common:error", "Error")}: {apiError}
        </div>
      )}

      <SummaryCards current={current} previous={previous} loading={loading} />

      <TokenAreaChart data={timeseries} loading={loading} granularity={filters.granularity} />
      <RequestVolumeChart data={timeseries} loading={loading} granularity={filters.granularity} />

      <DistributionRow
        providerBreakdown={providerBreakdown}
        modelBreakdown={modelBreakdown}
        channelBreakdown={channelBreakdown}
        loading={loading}
      />

      <DurationChart data={timeseries} loading={loading} granularity={filters.granularity} />
      <KnowledgeChart data={timeseries} loading={loading} granularity={filters.granularity} />

      <TopModelsTable data={modelBreakdown} loading={loading} />

      {/* Legacy records table */}
      <div>
        <h3 className="mb-3 text-sm font-semibold">{t("recentRecords")}</h3>
        {records.length === 0 && !recLoading ? (
          <EmptyState icon={BarChart3} title={t("emptyTitle")} description={t("emptyDescription")} />
        ) : (
          <div className="rounded-md border">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="px-4 py-3 text-left font-medium">{t("columns.agent")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.model")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.provider")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.channel")}</th>
                    <th className="px-4 py-3 text-right font-medium">{t("columns.input")}</th>
                    <th className="px-4 py-3 text-right font-medium">{t("columns.output")}</th>
                    <th className="px-4 py-3 text-right font-medium">{t("columns.cost")}</th>
                  </tr>
                </thead>
                <tbody>
                  {records.map((r, i) => (
                    <tr
                      key={i}
                      className="border-b last:border-0 hover:bg-muted/30 cursor-pointer"
                      onClick={() => setFilter("agentId", r.agentId)}
                    >
                      <td className="px-4 py-3 font-medium">{r.agentId}</td>
                      <td className="px-4 py-3"><Badge variant="outline">{r.model}</Badge></td>
                      <td className="px-4 py-3 text-muted-foreground">{r.provider}</td>
                      <td className="px-4 py-3 text-muted-foreground">—</td>
                      <td className="px-4 py-3 text-right text-muted-foreground">{formatTokens(r.inputTokens)}</td>
                      <td className="px-4 py-3 text-right text-muted-foreground">{formatTokens(r.outputTokens)}</td>
                      <td className="px-4 py-3 text-right">{formatCost(0)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pagination
              page={page}
              pageSize={pageSize}
              total={total}
              totalPages={totalPages}
              onPageChange={setPage}
              onPageSizeChange={() => {}}
            />
          </div>
        )}
      </div>
    </div>
  );
}

export function UsagePage() {
  return (
    <UsageFilterProvider>
      <AnalyticsDashboard />
    </UsageFilterProvider>
  );
}
