import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, ResponsiveContainer, Legend,
} from "recharts";
import { formatBucketTz } from "@/lib/format";
import { useUiStore } from "@/stores/use-ui-store";
import { ChartWrapper } from "./chart-wrapper";
import type { SnapshotTimeSeries } from "../hooks/use-usage-analytics";

interface KnowledgeChartProps {
  data: SnapshotTimeSeries[];
  loading?: boolean;
  granularity: "hour" | "day";
}

export function KnowledgeChart({ data, loading, granularity }: KnowledgeChartProps) {
  const { t } = useTranslation("usage");
  const timezone = useUiStore((s) => s.timezone);

  const hasData = data.some(
    (d) => d.memory_docs > 0 || d.memory_chunks > 0 || d.kg_entities > 0 || d.kg_relations > 0,
  );

  if (!loading && !hasData) return null;

  const chartData = useMemo(() => data.map((d) => ({
    label: formatBucketTz(d.bucket_time, timezone, granularity),
    memory_docs: d.memory_docs,
    memory_chunks: d.memory_chunks,
    kg_entities: d.kg_entities,
    kg_relations: d.kg_relations,
  })), [data, granularity, timezone]);

  return (
    <ChartWrapper
      title={t("analytics.knowledgeChart.title")}
      loading={loading}
      empty={false}
      emptyText={t("analytics.noData")}
    >
      <ResponsiveContainer width="100%" height={300}>
        <LineChart data={chartData} margin={{ top: 4, right: 16, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis dataKey="label" tick={{ fontSize: 11 }} tickLine={false} />
          <YAxis tick={{ fontSize: 11 }} width={40} />
          <Tooltip />
          <Legend />
          <Line type="monotone" dataKey="memory_docs" name={t("analytics.knowledgeChart.memoryDocs")} stroke="#E85D24" strokeWidth={2} dot={false} isAnimationActive={false} />
          <Line type="monotone" dataKey="memory_chunks" name={t("analytics.knowledgeChart.memoryChunks")} stroke="#F8D080" strokeWidth={2} dot={false} isAnimationActive={false} />
          <Line type="monotone" dataKey="kg_entities" name={t("analytics.knowledgeChart.kgEntities")} stroke="#E87820" strokeWidth={2} dot={false} isAnimationActive={false} />
          <Line type="monotone" dataKey="kg_relations" name={t("analytics.knowledgeChart.kgRelations")} stroke="#F0A020" strokeWidth={2} dot={false} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </ChartWrapper>
  );
}
