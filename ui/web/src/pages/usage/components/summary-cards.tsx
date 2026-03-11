import { useTranslation } from "react-i18next";
import { TrendingUp, TrendingDown, Minus } from "lucide-react";
import { formatTokens, formatCost } from "@/lib/format";
import type { SummaryData } from "../hooks/use-usage-analytics";

interface SummaryCardsProps {
  current: SummaryData;
  previous: SummaryData;
  loading?: boolean;
}

function trendPercent(curr: number, prev: number): number | null {
  if (prev === 0) return null;
  return ((curr - prev) / prev) * 100;
}

interface StatCardProps {
  label: string;
  value: string;
  trend: number | null;
  hint?: string;
}

function StatCard({ label, value, trend, hint }: StatCardProps) {
  const { t } = useTranslation("usage");
  const isUp = trend !== null && trend > 0;
  const isDown = trend !== null && trend < 0;

  return (
    <div className="rounded-lg border bg-card p-4">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 text-2xl font-semibold" title={hint}>{value}</p>
      {trend !== null ? (
        <div className={`mt-1 flex items-center gap-1 text-xs ${isUp ? "text-green-600" : isDown ? "text-red-500" : "text-muted-foreground"}`}>
          {isUp ? <TrendingUp className="h-3 w-3" /> : isDown ? <TrendingDown className="h-3 w-3" /> : <Minus className="h-3 w-3" />}
          <span>
            {isUp
              ? t("analytics.trendUp", { value: Math.abs(trend).toFixed(1) })
              : isDown
              ? t("analytics.trendDown", { value: trend.toFixed(1) })
              : "0%"}
          </span>
          <span className="text-muted-foreground">{t("analytics.vsPrevious")}</span>
        </div>
      ) : (
        <p className="mt-1 text-xs text-muted-foreground">{t("analytics.vsPrevious")}: N/A</p>
      )}
    </div>
  );
}

export function SummaryCards({ current, previous, loading }: SummaryCardsProps) {
  const { t } = useTranslation("usage");

  if (loading) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="rounded-lg border bg-card p-4 animate-pulse">
            <div className="h-3 w-20 rounded bg-muted mb-2" />
            <div className="h-7 w-16 rounded bg-muted" />
          </div>
        ))}
      </div>
    );
  }

  const allCostZero = current.cost === 0 && previous.cost === 0;
  const currentTokens = current.input_tokens + current.output_tokens;
  const previousTokens = previous.input_tokens + previous.output_tokens;

  const cards: StatCardProps[] = [
    {
      label: t("analytics.requests"),
      value: current.requests.toLocaleString(),
      trend: trendPercent(current.requests, previous.requests),
    },
    {
      label: t("analytics.tokens"),
      value: formatTokens(currentTokens),
      trend: trendPercent(currentTokens, previousTokens),
    },
    {
      label: t("analytics.cost"),
      value: formatCost(current.cost),
      trend: trendPercent(current.cost, previous.cost),
      hint: allCostZero ? t("analytics.configurePricing") : undefined,
    },
    {
      label: t("analytics.errors"),
      value: current.errors.toLocaleString(),
      trend: trendPercent(current.errors, previous.errors),
    },
    {
      label: t("analytics.uniqueUsers"),
      value: current.unique_users.toLocaleString(),
      trend: trendPercent(current.unique_users, previous.unique_users),
    },
  ];

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
      {cards.map((card) => (
        <StatCard key={card.label} {...card} />
      ))}
    </div>
  );
}
