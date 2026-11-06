import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";
import { Inbox } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/shared/empty-state";
import { useUiStore } from "@/stores/use-ui-store";
import { TokenAreaChart } from "@/pages/usage/components/token-area-chart";
import type { ProviderData } from "../hooks/use-providers";
import { useProviderHealth } from "../hooks/use-provider-health";
import {
  httpStatusOf,
  isUsageEmpty,
  parseUsageRange,
  useProviderUsage,
  type UsageErrorKind,
  type UsageRange,
} from "../hooks/use-provider-usage";
import { CooldownBanner, RecentErrorsList, UsageMetricCards, UsageRangeControl } from "./provider-usage-sections";
import { StaleDataNote, UsageErrorCard, UsageSkeleton } from "./provider-usage-states";

const RANGE_PARAM = "range";

/** Wall clock that ticks so relative copy (cooldown minutes) stays current. */
function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function ProviderUsageTab({ provider }: { provider: ProviderData }) {
  const { t } = useTranslation("providers");
  const [params, setParams] = useSearchParams();
  const timezone = useUiStore((s) => s.timezone);
  const now = useNow(15_000);

  // Range lives in the URL (like the tab) so reload/back keep it.
  const range = parseUsageRange(params.get(RANGE_PARAM));
  const setRange = (r: UsageRange) => {
    const next = new URLSearchParams(params);
    next.set(RANGE_PARAM, r);
    setParams(next, { replace: true });
  };

  const health = useProviderHealth(provider.id);
  const usage = useProviderUsage(provider.id, provider.name, range);
  const retry = () => {
    void usage.refetch();
    void health.refetch();
  };

  // A 503 from either endpoint means the gateway has no tracing store; say so instead of a generic error.
  const errorKind: UsageErrorKind | null =
    usage.errorKind === "http" && httpStatusOf(health.error) === 503 ? "tracing" : usage.errorKind;

  let body: ReactNode;
  if (!usage.hasData || !usage.detail) {
    body =
      errorKind && !usage.isLoading ? (
        <UsageErrorCard kind={errorKind} onRetry={retry} retrying={usage.isFetching} />
      ) : (
        <UsageSkeleton />
      );
  } else if (isUsageEmpty(usage.detail, usage.summary)) {
    body = (
      <EmptyState
        icon={Inbox}
        title={t("usage.empty.title")}
        description={t("usage.empty.description")}
        className="rounded-lg border py-12"
        action={
          range !== "30d" ? (
            <Button type="button" variant="outline" size="sm" onClick={() => setRange("30d")}>
              {t("usage.empty.action")}
            </Button>
          ) : undefined
        }
      />
    );
  } else {
    body = (
      <div className="space-y-4">
        <UsageMetricCards summary={usage.summary} detail={usage.detail} range={range} />
        <TokenAreaChart data={usage.series} granularity="day" title={t("usage.tokensPerDay")} />
        <RecentErrorsList errors={usage.detail.recent_errors} tz={timezone} now={now} />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <UsageRangeControl value={range} onChange={setRange} />
      {health.data?.cooling_down && <CooldownBanner health={health.data} now={now} tz={timezone} />}
      {usage.isStale && errorKind && (
        <StaleDataNote
          kind={errorKind}
          updatedAt={usage.dataUpdatedAt}
          tz={timezone}
          onRetry={retry}
          retrying={usage.isFetching}
        />
      )}
      {body}
    </div>
  );
}
