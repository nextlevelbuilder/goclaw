import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  ComposedChart, Bar, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, ResponsiveContainer, Legend,
} from "recharts";
import { formatBucketTz } from "@/lib/format";
import { useUiStore } from "@/stores/use-ui-store";
import { ChartWrapper } from "./chart-wrapper";
import type { SnapshotTimeSeries } from "../hooks/use-usage-analytics";

interface RequestVolumeChartProps {
  data: SnapshotTimeSeries[];
  loading?: boolean;
  granularity: "hour" | "day";
}

export function RequestVolumeChart({ data, loading, granularity }: RequestVolumeChartProps) {
  const { t } = useTranslation("usage");
  const timezone = useUiStore((s) => s.timezone);
  const isEmpty = !loading && data.length === 0;

  const chartData = useMemo(() => data.map((d) => ({
    label: formatBucketTz(d.bucket_time, timezone, granularity),
    request_count: d.request_count,
    error_count: d.error_count,
    errorRate: d.request_count > 0 ? +((d.error_count / d.request_count) * 100).toFixed(1) : 0,
  })), [data, granularity, timezone]);

  return (
    <ChartWrapper
      title={t("analytics.requestChart.title")}
      loading={loading}
      empty={isEmpty}
      emptyText={t("analytics.noData")}
    >
      <ResponsiveContainer width="100%" height={300}>
        <ComposedChart data={chartData} margin={{ top: 4, right: 40, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis dataKey="label" tick={{ fontSize: 11 }} tickLine={false} />
          <YAxis yAxisId="left" tick={{ fontSize: 11 }} width={40} />
          <YAxis yAxisId="right" orientation="right" tick={{ fontSize: 11 }} width={40} tickFormatter={(v) => `${v}`} />
          <Tooltip
            formatter={(value, name, props) => {
              const v = typeof value === "number" ? value : Number(value);
              const n = String(name);
              if (n === t("analytics.requestChart.requests")) return [v, n];
              const rate = (props.payload as { errorRate?: number })?.errorRate ?? 0;
              return [v, `${n} (${rate}% rate)`];
            }}
            labelFormatter={(label) => `${t("analytics.tooltip.date")}: ${label}`}
          />
          <Legend />
          <Bar
            yAxisId="left"
            dataKey="request_count"
            name={t("analytics.requestChart.requests")}
            fill="#E85D24"
            radius={[2, 2, 0, 0]}
            isAnimationActive={false}
          />
          <Line
            yAxisId="right"
            type="monotone"
            dataKey="error_count"
            name={t("analytics.requestChart.errors")}
            stroke="#ef4444"
            strokeWidth={2}
            dot={{ r: 3, fill: "#ef4444" }}
            isAnimationActive={false}
          />
        </ComposedChart>
      </ResponsiveContainer>
    </ChartWrapper>
  );
}
