import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip,
  ResponsiveContainer, Brush, Legend,
} from "recharts";
import { formatTokens, formatBucketTz } from "@/lib/format";
import { useUiStore } from "@/stores/use-ui-store";
import { ChartWrapper } from "./chart-wrapper";
import type { SnapshotTimeSeries } from "../hooks/use-usage-analytics";

interface TokenAreaChartProps {
  data: SnapshotTimeSeries[];
  loading?: boolean;
  granularity: "hour" | "day";
}

export function TokenAreaChart({ data, loading, granularity }: TokenAreaChartProps) {
  const { t } = useTranslation("usage");
  const timezone = useUiStore((s) => s.timezone);

  const isEmpty = !loading && data.length === 0;

  const { chartData, hasCache } = useMemo(() => {
    let cache = false;
    const mapped = data.map((d) => {
      if (d.cache_read_tokens > 0) cache = true;
      return { ...d, label: formatBucketTz(d.bucket_time, timezone, granularity) };
    });
    return { chartData: mapped, hasCache: cache };
  }, [data, granularity, timezone]);

  return (
    <ChartWrapper
      title={t("analytics.tokenChart.title")}
      loading={loading}
      empty={isEmpty}
      emptyText={t("analytics.noData")}
    >
      <ResponsiveContainer width="100%" height={300}>
        <AreaChart key={chartData[0]?.bucket_time ?? chartData.length} data={chartData} margin={{ top: 4, right: 16, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id="inputGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#E85D24" stopOpacity={0.3} />
              <stop offset="95%" stopColor="#E85D24" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="outputGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#10b981" stopOpacity={0.3} />
              <stop offset="95%" stopColor="#10b981" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis dataKey="label" tick={{ fontSize: 11 }} tickLine={false} />
          <YAxis tickFormatter={(v) => formatTokens(v)} tick={{ fontSize: 11 }} width={52} />
          <Tooltip
            formatter={(value, name) => [formatTokens(typeof value === "number" ? value : Number(value)), String(name)]}
            labelFormatter={(label) => `${t("analytics.tooltip.date")}: ${label}`}
          />
          <Legend />
          <Area
            type="monotone"
            dataKey="input_tokens"
            name={t("analytics.tokenChart.input")}
            stroke="#E85D24"
            fill="url(#inputGrad)"
            strokeWidth={2}
            isAnimationActive={false}
            stackId="tokens"
          />
          <Area
            type="monotone"
            dataKey="output_tokens"
            name={t("analytics.tokenChart.output")}
            stroke="#10b981"
            fill="url(#outputGrad)"
            strokeWidth={2}
            isAnimationActive={false}
            stackId="tokens"
          />
          {hasCache && (
            <Area
              type="monotone"
              dataKey="cache_read_tokens"
              name={t("analytics.tokenChart.cache")}
              stroke="#F0A020"
              fill="none"
              strokeWidth={1.5}
              isAnimationActive={false}
              strokeDasharray="4 2"
            />
          )}
          <Brush dataKey="label" height={20} stroke="#e5e7eb" />
        </AreaChart>
      </ResponsiveContainer>
    </ChartWrapper>
  );
}
