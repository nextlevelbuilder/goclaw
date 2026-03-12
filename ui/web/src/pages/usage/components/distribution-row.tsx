import { useTranslation } from "react-i18next";
import { DistributionDonut } from "./distribution-donut";
import { useUsageFilterContext } from "../context/usage-filter-context";
import type { SnapshotBreakdown } from "../hooks/use-usage-analytics";

interface DistributionRowProps {
  providerBreakdown: SnapshotBreakdown[];
  modelBreakdown: SnapshotBreakdown[];
  channelBreakdown: SnapshotBreakdown[];
  loading?: boolean;
}

export function DistributionRow({
  providerBreakdown,
  modelBreakdown,
  channelBreakdown,
  loading,
}: DistributionRowProps) {
  const { t } = useTranslation("usage");
  const { filters, toggleFilter } = useUsageFilterContext();

  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      <DistributionDonut
        title={t("analytics.distribution.provider")}
        data={providerBreakdown}
        loading={loading}
        activeValue={filters.provider}
        onSliceClick={(dim) => toggleFilter("provider", dim)}
        metric="llm_call_count"
      />
      <DistributionDonut
        title={t("analytics.distribution.model")}
        data={modelBreakdown}
        loading={loading}
        activeValue={filters.model}
        onSliceClick={(dim) => toggleFilter("model", dim)}
        metric="llm_call_count"
      />
      <DistributionDonut
        title={t("analytics.distribution.channel")}
        data={channelBreakdown}
        loading={loading}
        activeValue={filters.channel}
        onSliceClick={(dim) => toggleFilter("channel", dim)}
      />
    </div>
  );
}
