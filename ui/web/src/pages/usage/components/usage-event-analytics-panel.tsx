import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Cable, Sparkles, Terminal, Wrench } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatCost, formatDuration, formatTokens } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useUsageFilterContext } from "../context/usage-filter-context";
import { useUsageEventAnalytics, type UsageEventResourceType } from "../hooks/use-usage-event-analytics";

const RESOURCE_TABS: Array<{ value: UsageEventResourceType; icon: typeof Wrench }> = [
  { value: "tool", icon: Wrench },
  { value: "skill", icon: Sparkles },
  { value: "mcp_tool", icon: Cable },
  { value: "runtime_tool", icon: Terminal },
];

const EMPTY_SUMMARY = { calls: 0, errors: 0, input_tokens: 0, output_tokens: 0, total_tokens: 0, cost_usd: 0, avg_duration_ms: 0 };

export function UsageEventAnalyticsPanel() {
  const { t } = useTranslation("usage");
  const { filters } = useUsageFilterContext();
  const [resourceType, setResourceType] = useState<UsageEventResourceType>("tool");
  const { summary, rows, sourceRows, points, loading, error } = useUsageEventAnalytics(filters, resourceType);
  const current = summary ?? EMPTY_SUMMARY;
  const errorRate = current.calls > 0 ? (current.errors / current.calls) * 100 : 0;
  const activeBuckets = useMemo(() => points.filter((point) => point.calls > 0).length, [points]);
  const apiError = error instanceof Error ? error.message : error ? String(error) : null;

  return (
    <section className="space-y-4 rounded-md border p-3 sm:p-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h3 className="text-sm font-semibold">{t("analytics.events.title")}</h3>
          <p className="text-xs text-muted-foreground">{t("analytics.events.description")}</p>
        </div>
        <div className="grid grid-cols-2 gap-2 sm:flex">
          {RESOURCE_TABS.map((tab) => {
            const Icon = tab.icon;
            const active = resourceType === tab.value;
            return (
              <Button
                key={tab.value}
                type="button"
                variant={active ? "default" : "outline"}
                size="sm"
                className="gap-1.5 justify-start sm:justify-center"
                onClick={() => setResourceType(tab.value)}
              >
                <Icon className="h-3.5 w-3.5" />
                {t(`analytics.events.tabs.${tab.value}`)}
              </Button>
            );
          })}
        </div>
      </div>

      {apiError ? (
        <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
          {t("common:error", "Error")}: {apiError}
        </div>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        <Metric label={t("analytics.events.metrics.calls")} value={current.calls.toLocaleString()} loading={loading} />
        <Metric label={t("analytics.events.metrics.errorRate")} value={`${errorRate.toFixed(1)}%`} loading={loading} />
        <Metric label={t("analytics.events.metrics.tokens")} value={formatTokens(current.total_tokens)} loading={loading} />
        <Metric label={t("analytics.events.metrics.avgDuration")} value={formatDuration(current.avg_duration_ms)} loading={loading} />
        <Metric label={t("analytics.events.metrics.cost")} value={formatCost(current.cost_usd)} loading={loading} />
      </div>

      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <span>{t("analytics.events.activeBuckets", { count: activeBuckets })}</span>
        {sourceRows.length > 0 ? <span>·</span> : null}
        {sourceRows.map((row) => (
          <Badge key={row.key} variant="outline" className="font-normal">
            {row.key || t("analytics.events.unknownSource")}: {row.calls.toLocaleString()}
          </Badge>
        ))}
      </div>

      <div className="overflow-x-auto">
        <table className="w-full min-w-[720px] text-sm">
          <thead>
            <tr className="border-b bg-muted/50 text-xs text-muted-foreground">
              <th className="px-3 py-2 text-left font-medium">{t("analytics.events.table.resource")}</th>
              <th className="px-3 py-2 text-left font-medium">{t("analytics.events.table.source")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("analytics.events.table.calls")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("analytics.events.table.errors")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("analytics.events.table.errorRate")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("analytics.events.table.avgDuration")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("analytics.events.table.tokens")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("analytics.events.table.cost")}</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              Array.from({ length: 4 }).map((_, idx) => (
                <tr key={idx} className="border-b last:border-0">
                  <td colSpan={8} className="px-3 py-2"><div className="h-7 animate-pulse rounded bg-muted" /></td>
                </tr>
              ))
            ) : rows.length === 0 ? (
              <tr>
                <td colSpan={8} className="px-3 py-8 text-center text-muted-foreground">{t("analytics.events.empty")}</td>
              </tr>
            ) : (
              rows.map((row) => {
                const rowErrorRate = row.calls > 0 ? (row.errors / row.calls) * 100 : 0;
                return (
                  <tr key={`${row.resource_type}:${row.key}:${row.source}`} className={cn("border-b last:border-0", row.errors > 0 && "bg-destructive/5")}>
                    <td className="px-3 py-2 font-medium">{row.key || row.resource_name || "—"}</td>
                    <td className="px-3 py-2 text-muted-foreground">{row.source || "—"}</td>
                    <td className="px-3 py-2 text-right">{row.calls.toLocaleString()}</td>
                    <td className="px-3 py-2 text-right text-muted-foreground">{row.errors.toLocaleString()}</td>
                    <td className="px-3 py-2 text-right text-muted-foreground">{rowErrorRate.toFixed(1)}%</td>
                    <td className="px-3 py-2 text-right text-muted-foreground">{formatDuration(row.avg_duration_ms)}</td>
                    <td className="px-3 py-2 text-right text-muted-foreground">{formatTokens(row.total_tokens)}</td>
                    <td className="px-3 py-2 text-right">{formatCost(row.cost_usd)}</td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function Metric({ label, value, loading }: { label: string; value: string; loading: boolean }) {
  return (
    <div className="rounded-md border bg-card p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      {loading ? <div className="mt-2 h-6 animate-pulse rounded bg-muted" /> : <div className="mt-1 text-lg font-semibold">{value}</div>}
    </div>
  );
}
