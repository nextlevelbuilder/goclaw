import { useTranslation } from "react-i18next";
import { AlertTriangle, CircleAlert } from "lucide-react";
import { formatCost, formatTokens, resolveTimezone } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { SummaryData } from "@/pages/usage/hooks/use-usage-analytics";
import { cooldownRemainingMinutes, type ProviderHealth } from "../hooks/use-provider-health";
import {
  USAGE_RANGES,
  errorClassKey,
  errorRate,
  formatErrorRate,
  formatLatency,
  rangeDays,
  type ProviderRecentError,
  type ProviderUsageDetail,
  type UsageRange,
} from "../hooks/use-provider-usage";

/** Locale- and timezone-aware clock time; includes the date unless it is today. */
export function formatEventTime(iso: string | number, tz: string, locale: string, now = Date.now()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const timeZone = resolveTimezone(tz);
  const dayFmt = new Intl.DateTimeFormat(locale, { timeZone, year: "numeric", month: "2-digit", day: "2-digit" });
  const sameDay = dayFmt.format(d) === dayFmt.format(new Date(now));
  return new Intl.DateTimeFormat(locale, {
    timeZone,
    hour: "2-digit",
    minute: "2-digit",
    ...(sameDay ? {} : { month: "short", day: "numeric" }),
  }).format(d);
}

const RANGE_LABEL_KEY: Record<UsageRange, string> = {
  "24h": "usage.range.last24h",
  "7d": "usage.range.last7d",
  "30d": "usage.range.last30d",
};

export function UsageRangeControl({ value, onChange }: { value: UsageRange; onChange: (r: UsageRange) => void }) {
  const { t } = useTranslation("providers");
  return (
    <div role="group" aria-label={t("usage.range.label")} className="inline-flex rounded-lg bg-muted p-[3px]">
      {USAGE_RANGES.map((r) => (
        <button
          key={r}
          type="button"
          aria-pressed={value === r}
          onClick={() => onChange(r)}
          className={cn(
            "min-h-8 rounded-md px-3 text-sm font-medium text-muted-foreground transition-colors pointer-coarse:min-h-11",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            value === r && "bg-background text-foreground shadow-sm",
          )}
        >
          {t(RANGE_LABEL_KEY[r])}
        </button>
      ))}
    </div>
  );
}

export function CooldownBanner({ health, now, tz }: { health: ProviderHealth; now: number; tz: string }) {
  const { t, i18n } = useTranslation("providers");
  const minutes = cooldownRemainingMinutes(health, now);
  const reason = t(errorClassKey(health.last_error_class));
  const title = health.updated_at
    ? t("usage.banner.since", { reason, time: formatEventTime(health.updated_at, tz, i18n.language, now) })
    : reason;

  return (
    <div
      role="status"
      className="flex items-start gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm"
    >
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" aria-hidden="true" />
      <div className="min-w-0 space-y-0.5">
        <p className="font-medium">{title}</p>
        <p className="text-muted-foreground">
          {minutes != null && minutes > 0
            ? t("usage.banner.resetIn", { minutes })
            : t("usage.banner.resetSoon")}
        </p>
        {health.consecutive_failures > 0 && (
          <p className="text-xs text-muted-foreground">
            {t("usage.banner.failures", { count: health.consecutive_failures })}
          </p>
        )}
      </div>
    </div>
  );
}

function MetricCard({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return (
    <div className="rounded-lg border bg-card p-3 sm:p-4">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>
      {detail && <p className="mt-0.5 text-xs text-muted-foreground tabular-nums">{detail}</p>}
    </div>
  );
}

export function UsageMetricCards({
  summary,
  detail,
  range,
}: {
  summary: SummaryData | null;
  detail: ProviderUsageDetail;
  range: UsageRange;
}) {
  const { t, i18n } = useTranslation("providers");
  const num = new Intl.NumberFormat(i18n.language);
  const tokens = (summary?.input_tokens ?? 0) + (summary?.output_tokens ?? 0);
  const requests = summary?.requests ?? 0;
  const perDay = Math.round(requests / rangeDays(range));

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <MetricCard
        label={t("usage.metrics.tokens")}
        value={formatTokens(tokens)}
        detail={t("usage.metrics.costEstimated", { cost: formatCost(summary?.cost) })}
      />
      <MetricCard
        label={t("usage.metrics.requests")}
        value={num.format(requests)}
        detail={t("usage.metrics.perDay", { value: num.format(perDay) })}
      />
      <MetricCard
        label={t("usage.metrics.errorRate")}
        value={formatErrorRate(errorRate(detail.errors, detail.calls))}
        detail={
          // No call spans (e.g. pruned by trace retention): the rate reads "—" without a "0 of 0" line.
          detail.calls > 0
            ? t("usage.metrics.errorRateDetail", { errors: num.format(detail.errors), calls: num.format(detail.calls) })
            : undefined
        }
      />
      <MetricCard
        label={t("usage.metrics.latency")}
        value={formatLatency(detail.latency_ms.p50)}
        detail={t("usage.metrics.p95", { value: formatLatency(detail.latency_ms.p95) })}
      />
    </div>
  );
}

export function RecentErrorsList({ errors, tz, now }: { errors: ProviderRecentError[]; tz: string; now: number }) {
  const { t, i18n } = useTranslation("providers");
  return (
    <section className="rounded-lg border bg-card p-3 sm:p-4">
      <h3 className="mb-2 text-sm font-semibold">{t("usage.recentErrors.title")}</h3>
      {errors.length === 0 ? (
        <p className="py-4 text-center text-sm text-muted-foreground">{t("usage.recentErrors.empty")}</p>
      ) : (
        <ul className="divide-y">
          {errors.map((e) => (
            <li key={`${e.at}-${e.error_class}`} className="flex items-center gap-3 py-2 text-sm">
              <time dateTime={e.at} className="w-24 shrink-0 text-xs text-muted-foreground tabular-nums">
                {formatEventTime(e.at, tz, i18n.language, now)}
              </time>
              <span className="flex min-w-0 flex-1 items-center gap-1.5">
                <CircleAlert className="h-3.5 w-3.5 shrink-0 text-destructive" aria-hidden="true" />
                <span className="truncate">{t(errorClassKey(e.error_class))}</span>
              </span>
              <span
                className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs font-medium tabular-nums"
                aria-label={t("usage.recentErrors.count", { count: e.count })}
              >
                {e.count}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
