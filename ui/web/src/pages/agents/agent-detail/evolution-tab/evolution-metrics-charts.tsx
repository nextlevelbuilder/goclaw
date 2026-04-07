import { useTranslation } from "react-i18next";
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid,
  Tooltip, ResponsiveContainer,
} from "recharts";
import type { ToolAggregate, RetrievalAggregate } from "@/types/evolution";

interface EvolutionMetricsChartsProps {
  toolAggs: ToolAggregate[];
  retrievalAggs: RetrievalAggregate[];
  loading: boolean;
}

export function EvolutionMetricsCharts({ toolAggs, retrievalAggs, loading }: EvolutionMetricsChartsProps) {
  const { t } = useTranslation("agents");

  if (loading) {
    return <div className="h-[200px] animate-pulse rounded-md bg-muted" />;
  }

  return (
    <div className="space-y-6">
      {/* Tool Success Rate */}
      <div className="space-y-2">
        <h4 className="text-sm font-medium">{t("detail.evolution.toolSuccess")}</h4>
        {toolAggs.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t("detail.evolution.noMetrics")}</p>
        ) : (
          <ResponsiveContainer width="100%" height={220}>
            <BarChart data={toolAggs} margin={{ top: 4, right: 20, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
              <XAxis dataKey="tool_name" tick={{ fontSize: 11 }} tickLine={false} />
              <YAxis domain={[0, 100]} tick={{ fontSize: 11 }} width={36} tickFormatter={(v) => `${v}%`} />
              <Tooltip
                formatter={(value, _name, props) => {
                  const v = Number(value ?? 0);
                  const p = props?.payload as ToolAggregate | undefined;
                  return [`${v.toFixed(1)}%`, p ? `${p.call_count} calls, ${p.avg_duration_ms.toFixed(0)}ms avg` : ""];
                }}
              />
              <Bar
                dataKey="success_rate"
                name="Success Rate"
                radius={[3, 3, 0, 0]}
                isAnimationActive={false}
                fill="#22c55e"
              />
            </BarChart>
          </ResponsiveContainer>
        )}
      </div>

      {/* Retrieval Quality */}
      <div className="space-y-2">
        <h4 className="text-sm font-medium">{t("detail.evolution.retrievalQuality")}</h4>
        {retrievalAggs.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t("detail.evolution.noMetrics")}</p>
        ) : (
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={retrievalAggs} margin={{ top: 4, right: 20, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
              <XAxis dataKey="source" tick={{ fontSize: 11 }} tickLine={false} />
              <YAxis domain={[0, 100]} tick={{ fontSize: 11 }} width={36} tickFormatter={(v) => `${v}%`} />
              <Tooltip
                formatter={(value, _name, props) => {
                  const v = Number(value ?? 0);
                  const p = props?.payload as RetrievalAggregate | undefined;
                  return [`${v.toFixed(1)}%`, p ? `${p.query_count} queries, score ${p.avg_score.toFixed(2)}` : ""];
                }}
              />
              <Bar
                dataKey="usage_rate"
                name="Usage Rate"
                fill="#3b82f6"
                radius={[3, 3, 0, 0]}
                isAnimationActive={false}
              />
            </BarChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}
