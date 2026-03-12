import { useTranslation } from "react-i18next";
import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer, Legend } from "recharts";
import type { PieSectorDataItem } from "recharts/types/polar/Pie";
import { ChartWrapper } from "./chart-wrapper";
import type { SnapshotBreakdown } from "../hooks/use-usage-analytics";

const PALETTE = [
  "#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6",
  "#06b6d4", "#f97316", "#ec4899", "#84cc16", "#6366f1",
];

const MAX_SLICES = 8;

interface DistributionDonutProps {
  title: string;
  data: SnapshotBreakdown[];
  loading?: boolean;
  activeValue?: string;
  onSliceClick?: (dimension: string) => void;
  metric?: "request_count" | "llm_call_count";
}

interface SliceEntry {
  name: string;
  value: number;
  calls: number;
}

export function DistributionDonut({
  title,
  data,
  loading,
  activeValue,
  onSliceClick,
  metric = "request_count",
}: DistributionDonutProps) {
  const { t } = useTranslation("usage");
  const isEmpty = !loading && data.length === 0;

  const sorted = [...data].sort((a, b) => b[metric] - a[metric]);
  const top = sorted.slice(0, MAX_SLICES);
  const rest = sorted.slice(MAX_SLICES);

  const slices: SliceEntry[] = top.map((d) => ({ name: d.key, value: d[metric], calls: d[metric] }));

  if (rest.length > 0) {
    const otherCount = rest.reduce((sum, d) => sum + d[metric], 0);
    slices.push({ name: t("analytics.distribution.other"), value: otherCount, calls: otherCount });
  }

  const total = slices.reduce((sum, s) => sum + s.value, 0);

  const handleClick = (entry: PieSectorDataItem) => {
    const name = entry.name as string | undefined;
    if (!name || name === t("analytics.distribution.other")) return;
    onSliceClick?.(name);
  };

  return (
    <ChartWrapper title={title} loading={loading} empty={isEmpty} emptyText={t("analytics.noData")} height={260}>
      <ResponsiveContainer width="100%" height={260}>
        <PieChart>
          <Pie
            data={slices}
            cx="50%"
            cy="45%"
            innerRadius={55}
            outerRadius={85}
            paddingAngle={2}
            dataKey="value"
            onClick={handleClick}
            style={{ cursor: onSliceClick ? "pointer" : "default" }}
            label={false}
          >
            {slices.map((entry, idx) => (
              <Cell
                key={entry.name}
                fill={PALETTE[idx % PALETTE.length]}
                stroke={activeValue === entry.name ? "#1d4ed8" : "transparent"}
                strokeWidth={activeValue === entry.name ? 3 : 0}
                opacity={activeValue && activeValue !== entry.name ? 0.5 : 1}
              />
            ))}
          </Pie>
          <text x="50%" y="44%" textAnchor="middle" dominantBaseline="middle" className="fill-foreground text-sm font-semibold">
            {total.toLocaleString()}
          </text>
          <text x="50%" y="52%" textAnchor="middle" dominantBaseline="middle" className="fill-muted-foreground text-xs">
            {t("analytics.distribution.calls")}
          </text>
          <Tooltip
            formatter={(value, name) => {
              const v = typeof value === "number" ? value : Number(value) || 0;
              const pct = total > 0 ? ((v / total) * 100).toFixed(1) : "0";
              return [`${v.toLocaleString()} (${pct}%)`, String(name)];
            }}
          />
          <Legend
            iconType="circle"
            iconSize={8}
            formatter={(value: string) => <span className="text-xs">{value}</span>}
          />
        </PieChart>
      </ResponsiveContainer>
    </ChartWrapper>
  );
}
