import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, Route } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/shared/empty-state";
import { cn } from "@/lib/utils";
import { strategyLabelKey } from "./agent-display-utils";
import { getRouteReadiness } from "./chatgpt-oauth-quota-utils";
import { CodexPoolMemberCard } from "./codex-pool-member-card";
import { CodexPoolRecentRequestsList } from "./codex-pool-recent-requests-list";

export type { CodexPoolEntry } from "./codex-pool-entry-types";
import type { CodexPoolActivityPanelProps, CodexPoolRecentRequestsPanelProps } from "./codex-pool-entry-types";

function MonitorStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="h-full rounded-md border bg-background/70 px-2 py-1 xl:px-2.5 xl:py-1.5">
      <p className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground xl:text-[10px]">
        {label}
      </p>
      <p className="mt-0.5 text-[13px] font-semibold leading-tight tabular-nums xl:mt-1 xl:text-sm">
        {value}
      </p>
    </div>
  );
}

export function CodexPoolActivityPanel({
  entries,
  strategy,
  recentRequests,
  statsSampleSize,
  fetching,
  showProviderLinks = true,
  onRefresh,
  className,
}: CodexPoolActivityPanelProps) {
  const { t } = useTranslation("agents");

  const routeEntries = useMemo(
    () =>
      entries.map((entry) => ({
        ...entry,
        routeReadiness: getRouteReadiness(entry.availability, entry.quota),
      })),
    [entries],
  );
  const blockedEntries = routeEntries.filter(
    (entry) => entry.routeReadiness === "blocked",
  );
  const directObservedProviders = routeEntries.filter(
    (entry) =>
      entry.routeReadiness !== "blocked" && entry.directSelectionCount > 0,
  ).length;
  const failoverOnlyProviders = routeEntries.filter(
    (entry) => entry.directSelectionCount === 0 && entry.failoverServeCount > 0,
  ).length;

  return (
    <Card className={cn("flex h-full min-h-0 flex-col gap-0 overflow-hidden", className)}>
      <CardHeader className="border-b bg-muted/20 px-3 py-2.5 lg:px-4 lg:py-3 [@media(max-height:760px)]:py-1.5">
        <div className="flex flex-col gap-1.5 sm:flex-row sm:items-start sm:justify-between [@media(max-height:760px)]:gap-1">
          <div>
            <CardTitle className="text-sm sm:text-base [@media(max-height:760px)]:text-[15px]">
              {t("chatgptOAuthRouting.activityTitle")}
            </CardTitle>
            <p className="hidden text-xs text-muted-foreground xl:block [@media(max-height:760px)]:hidden">
              {t("chatgptOAuthRouting.activityDescription")}
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2 [@media(max-height:760px)]:gap-1.5">
            <Badge variant="outline" className="h-6 px-2 text-[11px] [@media(max-height:760px)]:h-5">
              {t(strategyLabelKey(strategy))}
            </Badge>
            {blockedEntries.length > 0 && (
              <Badge variant="warning" className="h-6 px-2 text-[11px] [@media(max-height:760px)]:h-5">
                {t("chatgptOAuthRouting.blockedNowTitle")} {blockedEntries.length}
              </Badge>
            )}
            {failoverOnlyProviders > 0 && (
              <Badge variant="warning" className="h-6 px-2 text-[11px] [@media(max-height:760px)]:h-5">
                {t("chatgptOAuthRouting.failoverOnlyProviders", {
                  count: failoverOnlyProviders,
                })}
              </Badge>
            )}
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 px-2.5 [@media(max-height:760px)]:h-7 [@media(max-height:760px)]:px-2"
              onClick={onRefresh}
              disabled={fetching}
            >
              <RefreshCw className={`h-4 w-4${fetching ? " animate-spin" : ""}`} />
              {t("chatgptOAuthRouting.refreshEvidence")}
            </Button>
          </div>
        </div>
      </CardHeader>

      <CardContent className="flex min-h-0 flex-1 flex-col gap-2.5 overflow-hidden px-3 py-2.5 lg:px-4 lg:py-3 [@media(max-height:760px)]:gap-2 [@media(max-height:760px)]:py-2">
        <div className="grid gap-1.5 sm:grid-cols-2 xl:grid-cols-4 [@media(max-height:760px)]:gap-1">
          <MonitorStat
            label={t("chatgptOAuthRouting.metrics.poolSize")}
            value={String(entries.length)}
          />
          <MonitorStat
            label={t("chatgptOAuthRouting.metrics.observedSample")}
            value={String(statsSampleSize)}
          />
          <MonitorStat
            label={t("chatgptOAuthRouting.metrics.observedRotation")}
            value={`${directObservedProviders}/${entries.length}`}
          />
          <MonitorStat
            label={t("chatgptOAuthRouting.metrics.failovers")}
            value={String(recentRequests.filter((r) => r.used_failover).length)}
          />
        </div>

        <section className="shrink-0 rounded-lg border bg-muted/5 p-2 [@media(max-height:760px)]:p-1.5">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-medium [@media(max-height:760px)]:text-[13px]">
              {t("chatgptOAuthRouting.sequenceTitle")}
            </h3>
            <Badge variant="outline">
              {t("chatgptOAuthRouting.recentRequestsCount", {
                count: recentRequests.length,
              })}
            </Badge>
          </div>
          <CodexPoolRecentRequestsList
            recentRequests={recentRequests}
            loading={fetching && recentRequests.length === 0}
            compact
            className="mt-1.5 [@media(max-height:760px)]:mt-1"
          />
        </section>

        <section className="flex min-h-0 flex-1 flex-col gap-2.5 [@media(max-height:760px)]:gap-2">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-medium [@media(max-height:760px)]:text-[13px]">
              {t("chatgptOAuthRouting.poolMembersTitle")}
            </h3>
            <Badge variant="outline">
              {t("chatgptOAuthRouting.selectedCount", { count: entries.length })}
            </Badge>
          </div>

          {entries.length === 0 ? (
            <div className="rounded-lg border border-dashed bg-muted/5">
              <EmptyState
                icon={Route}
                title={t("chatgptOAuthRouting.noReadyExtras")}
                description={t("chatgptOAuthRouting.extraSelectableHint")}
                className="py-6"
              />
            </div>
          ) : (
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain pr-1">
              <div className="grid auto-rows-min content-start gap-2.5 [grid-template-columns:repeat(auto-fit,minmax(min(100%,12.25rem),1fr))] lg:[grid-template-columns:repeat(auto-fit,minmax(min(100%,14rem),1fr))] xl:[grid-template-columns:repeat(auto-fit,minmax(min(100%,15rem),1fr))] [@media(max-height:760px)]:gap-2 [@media(max-height:760px)]:[grid-template-columns:repeat(auto-fit,minmax(min(100%,12.25rem),1fr))]">
                {entries.map((entry) => (
                  <CodexPoolMemberCard
                    key={entry.name}
                    entry={entry}
                    showProviderLinks={showProviderLinks}
                  />
                ))}
              </div>
            </div>
          )}
        </section>
      </CardContent>
    </Card>
  );
}

export function CodexPoolRecentRequestsPanel({
  recentRequests,
  loading,
  className,
}: CodexPoolRecentRequestsPanelProps) {
  const { t } = useTranslation("agents");

  return (
    <Card className={cn("flex min-h-0 flex-col gap-0 overflow-hidden", className)}>
      <CardHeader className="border-b bg-muted/20 px-4 py-3">
        <div className="flex items-center justify-between gap-2">
          <div>
            <CardTitle className="text-base">
              {t("chatgptOAuthRouting.sequenceTitle")}
            </CardTitle>
            <p className="text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.sequenceDescription")}
            </p>
          </div>
          <Badge variant="outline">
            {t("chatgptOAuthRouting.recentRequestsCount", {
              count: recentRequests.length,
            })}
          </Badge>
        </div>
      </CardHeader>

      <CardContent className="flex min-h-0 flex-1 flex-col overflow-hidden px-4 py-3">
        <CodexPoolRecentRequestsList recentRequests={recentRequests} loading={loading} />
      </CardContent>
    </Card>
  );
}
