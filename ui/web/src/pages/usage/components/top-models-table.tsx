import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowUpDown, ArrowUp, ArrowDown } from "lucide-react";
import { formatTokens, formatCost, formatDuration } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useUsageFilterContext } from "../context/usage-filter-context";
import type { SnapshotBreakdown } from "../hooks/use-usage-analytics";

type SortKey = "llm_call_count" | "input_tokens" | "output_tokens" | "avg_duration_ms" | "total_cost";

interface TopModelsTableProps {
  data: SnapshotBreakdown[];
  loading?: boolean;
}

export function TopModelsTable({ data, loading }: TopModelsTableProps) {
  const { t } = useTranslation("usage");
  const { filters, toggleFilter } = useUsageFilterContext();
  const [sortKey, setSortKey] = useState<SortKey>("llm_call_count");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");

  const handleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((d) => (d === "desc" ? "asc" : "desc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  };

  const sorted = [...data].sort((a, b) => {
    const av = a[sortKey] ?? 0;
    const bv = b[sortKey] ?? 0;
    return sortDir === "desc" ? bv - av : av - bv;
  });

  function SortIcon({ col }: { col: SortKey }) {
    if (sortKey !== col) return <ArrowUpDown className="ml-1 h-3 w-3 opacity-40" />;
    return sortDir === "desc"
      ? <ArrowDown className="ml-1 h-3 w-3" />
      : <ArrowUp className="ml-1 h-3 w-3" />;
  }

  function ThSort({ col, label }: { col: SortKey; label: string }) {
    return (
      <th
        className="px-3 py-2 text-right font-medium cursor-pointer select-none hover:text-foreground whitespace-nowrap"
        onClick={() => handleSort(col)}
      >
        <span className="inline-flex items-center justify-end">
          {label}
          <SortIcon col={col} />
        </span>
      </th>
    );
  }

  if (loading) {
    return (
      <div className="rounded-lg border bg-card p-4">
        <h3 className="mb-3 text-sm font-semibold">{t("analytics.topModels.title")}</h3>
        <div className="space-y-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="h-8 animate-pulse rounded bg-muted" />
          ))}
        </div>
      </div>
    );
  }

  if (data.length === 0) return null;

  return (
    <div className="rounded-lg border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b">
        <h3 className="text-sm font-semibold">{t("analytics.topModels.title")}</h3>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b bg-muted/50 text-xs text-muted-foreground">
              <th className="px-3 py-2 text-left font-medium">{t("analytics.topModels.model")}</th>
              <th className="px-3 py-2 text-left font-medium">{t("analytics.topModels.provider")}</th>
              <ThSort col="llm_call_count" label={t("analytics.topModels.llmCalls")} />
              <ThSort col="input_tokens" label={t("analytics.topModels.inputTokens")} />
              <ThSort col="output_tokens" label={t("analytics.topModels.outputTokens")} />
              <ThSort col="avg_duration_ms" label={t("analytics.topModels.avgDuration")} />
              <ThSort col="total_cost" label={t("analytics.topModels.cost")} />
            </tr>
          </thead>
          <tbody>
            {sorted.map((row) => {
              const isActive = filters.model === row.key;
              const [model, provider] = row.key.includes("/")
                ? row.key.split("/", 2)
                : [row.key, "—"];
              return (
                <tr
                  key={row.key}
                  className={cn(
                    "border-b last:border-0 hover:bg-muted/30 cursor-pointer transition-colors",
                    isActive && "bg-primary/5",
                  )}
                  onClick={() => toggleFilter("model", row.key)}
                >
                  <td className="px-3 py-2 font-medium">{model}</td>
                  <td className="px-3 py-2 text-muted-foreground">{provider}</td>
                  <td className="px-3 py-2 text-right">{row.llm_call_count.toLocaleString()}</td>
                  <td className="px-3 py-2 text-right text-muted-foreground">{formatTokens(row.input_tokens)}</td>
                  <td className="px-3 py-2 text-right text-muted-foreground">{formatTokens(row.output_tokens)}</td>
                  <td className="px-3 py-2 text-right text-muted-foreground">{formatDuration(row.avg_duration_ms)}</td>
                  <td className="px-3 py-2 text-right">{formatCost(row.total_cost)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
