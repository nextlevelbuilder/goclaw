import { useMemo } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowUpRight, RefreshCw, Route } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/shared/empty-state";
import { formatDuration, formatRelativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { ChatGPTOAuthAvailability } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import type { ChatGPTOAuthProviderQuota } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import { ChatGPTOAuthQuotaStrip } from "./chatgpt-oauth-quota-strip";
import {
  getQuotaFailureKind,
  getRouteReadiness,
} from "./chatgpt-oauth-quota-utils";
import type { CodexPoolRecentRequest } from "./hooks/use-codex-pool-activity";

export interface CodexPoolEntry {
  name: string;
  label: string;
  availability: ChatGPTOAuthAvailability;
  role: "preferred" | "extra";
  requestCount: number;
  directSelectionCount: number;
  failoverServeCount: number;
  lastSelectedAt?: string;
  lastFailoverAt?: string;
  lastUsedAt?: string;
  providerHref?: string;
  quota?: ChatGPTOAuthProviderQuota | null;
}

interface CodexPoolActivityPanelProps {
  entries: CodexPoolEntry[];
  strategy: "manual" | "round_robin";
  recentRequests: CodexPoolRecentRequest[];
  loading: boolean;
  fetching: boolean;
  showProviderLinks?: boolean;
  onRefresh: () => void;
  className?: string;
}

function availabilityVariant(
  availability: ChatGPTOAuthAvailability,
): "success" | "warning" | "outline" {
  if (availability === "ready") return "success";
  if (availability === "needs_sign_in") return "warning";
  return "outline";
}

function requestStatusVariant(
  status: string,
): "success" | "destructive" | "info" | "secondary" {
  if (status === "ok" || status === "success" || status === "completed")
    return "success";
  if (status === "error" || status === "failed") return "destructive";
  if (status === "running" || status === "pending") return "info";
  return "secondary";
}

function routeBadgeVariant(
  state: ReturnType<typeof getRouteReadiness>,
): "success" | "warning" | "outline" | "destructive" {
  if (state === "healthy") return "success";
  if (state === "fallback") return "warning";
  if (state === "checking") return "outline";
  return "destructive";
}

function routeLabelKey(state: ReturnType<typeof getRouteReadiness>): string {
  if (state === "healthy") return "chatgptOAuthRouting.routerActiveTitle";
  if (state === "fallback") return "chatgptOAuthRouting.fallbackTitle";
  if (state === "checking") return "chatgptOAuthRouting.checkingTitle";
  return "chatgptOAuthRouting.blockedNowTitle";
}

function MonitorStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="h-full rounded-lg border bg-background/70 px-2.5 py-1.5">
      <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </p>
      <p className="mt-1 text-sm font-semibold leading-tight">
        {value}
      </p>
    </div>
  );
}

function MemberMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="h-full rounded-md border bg-background/70 px-2.5 py-1.5">
      <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </p>
      <p className="mt-1 text-sm font-semibold leading-tight">{value}</p>
    </div>
  );
}

export function CodexPoolActivityPanel({
  entries,
  strategy,
  recentRequests,
  loading,
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
        failureKind: getQuotaFailureKind(entry.quota),
      })),
    [entries],
  );
  const healthyEntries = routeEntries.filter(
    (entry) => entry.routeReadiness === "healthy",
  );
  const blockedEntries = routeEntries.filter(
    (entry) => entry.routeReadiness === "blocked",
  );
  const routerActiveEntries = healthyEntries;
  const observedProviders = routeEntries.filter(
    (entry) =>
      entry.routeReadiness !== "blocked" && entry.directSelectionCount > 0,
  ).length;
  const failoverOnlyProviders = routeEntries.filter(
    (entry) => entry.directSelectionCount === 0 && entry.failoverServeCount > 0,
  ).length;

  return (
    <Card className={cn("flex h-full min-h-0 flex-col gap-0 overflow-hidden", className)}>
      <CardHeader className="border-b bg-muted/20 px-4 py-3">
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline">
                  {strategy === "round_robin"
                    ? t("chatgptOAuthRouting.strategy.roundRobin")
                    : t("chatgptOAuthRouting.strategy.manual")}
                </Badge>
                <Badge
                  variant={
                    routerActiveEntries.length > 0 ? "success" : "warning"
                  }
                >
                  {t("chatgptOAuthRouting.routerActiveTitle")}{" "}
                  {routerActiveEntries.length}
                </Badge>
                {blockedEntries.length > 0 && (
                  <Badge variant="warning">
                    {t("chatgptOAuthRouting.blockedNowTitle")}{" "}
                    {blockedEntries.length}
                  </Badge>
                )}
                {failoverOnlyProviders > 0 && (
                  <Badge variant="warning">
                    {t("chatgptOAuthRouting.failoverOnlyProviders", {
                      count: failoverOnlyProviders,
                    })}
                  </Badge>
                )}
              </div>
              <div>
                <CardTitle className="text-base">
                  {t("chatgptOAuthRouting.activityTitle")}
                </CardTitle>
                <p className="text-sm text-muted-foreground">
                  {t("chatgptOAuthRouting.activityDescription")}
                </p>
              </div>
            </div>

            <Button
              type="button"
              variant="outline"
              size="sm"
              className="gap-1.5"
              onClick={onRefresh}
              disabled={fetching}
            >
              <RefreshCw
                className={`h-4 w-4${fetching ? " animate-spin" : ""}`}
              />
              {t("chatgptOAuthRouting.refreshEvidence")}
            </Button>
          </div>

          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
            <MonitorStat
              label={t("chatgptOAuthRouting.metrics.poolSize")}
              value={String(entries.length)}
            />
            <MonitorStat
              label={t("chatgptOAuthRouting.metrics.routerActiveAccounts")}
              value={String(routerActiveEntries.length)}
            />
            <MonitorStat
              label={t("chatgptOAuthRouting.metrics.observedRotation")}
              value={`${observedProviders}/${entries.length}`}
            />
            <MonitorStat
              label={t("chatgptOAuthRouting.metrics.failovers")}
              value={String(
                recentRequests.filter((request) => request.used_failover)
                  .length,
              )}
            />
          </div>
        </div>
      </CardHeader>

      <CardContent className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain px-4 py-3">
        <section className="flex shrink-0 flex-col gap-3">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-medium">
              {t("chatgptOAuthRouting.poolMembersTitle")}
            </h3>
            <Badge variant="outline">
              {t("chatgptOAuthRouting.selectedCount", {
                count: entries.length,
              })}
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
            <div className="max-h-[min(17rem,36dvh)] overflow-y-auto overscroll-contain pr-1">
              <div className="grid auto-rows-min content-start justify-start gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(100%,18.5rem),18.5rem))]">
                {routeEntries.map((entry) => (
                  <div
                    key={entry.name}
                    className="rounded-lg border bg-muted/10 p-3"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0 space-y-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="truncate text-sm font-medium">
                            {entry.label}
                          </span>
                          <Badge
                            variant={
                              entry.role === "preferred"
                                ? "secondary"
                                : "outline"
                            }
                          >
                            {t(`chatgptOAuthRouting.role.${entry.role}`)}
                          </Badge>
                          <Badge
                            variant={availabilityVariant(entry.availability)}
                          >
                            {t(
                              `chatgptOAuthRouting.status.${entry.availability}`,
                            )}
                          </Badge>
                          <Badge
                            variant={routeBadgeVariant(entry.routeReadiness)}
                          >
                            {t(routeLabelKey(entry.routeReadiness))}
                          </Badge>
                        </div>
                        <p className="truncate font-mono text-xs text-muted-foreground">
                          {entry.name}
                        </p>
                      </div>

                      {showProviderLinks && entry.providerHref && (
                        <Button
                          asChild
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 shrink-0"
                        >
                          <Link
                            to={entry.providerHref}
                            aria-label={t("chatgptOAuthRouting.openProvider")}
                            title={t("chatgptOAuthRouting.openProvider")}
                          >
                            <ArrowUpRight className="h-4 w-4" />
                          </Link>
                        </Button>
                      )}
                    </div>

                    <ChatGPTOAuthQuotaStrip
                      quota={entry.quota}
                      className="mt-2.5"
                      compact
                    />

                    <div className="mt-2.5 grid gap-2 sm:grid-cols-3">
                      <MemberMetric
                        label={t("chatgptOAuthRouting.monitorDirectUseLabel")}
                        value={String(entry.directSelectionCount)}
                      />
                      <MemberMetric
                        label={t("chatgptOAuthRouting.monitorFailoverLabel")}
                        value={String(entry.failoverServeCount)}
                      />
                      <MemberMetric
                        label={t("chatgptOAuthRouting.lastSeenLabel")}
                        value={
                          entry.lastUsedAt
                            ? formatRelativeTime(entry.lastUsedAt)
                            : t("chatgptOAuthRouting.never")
                        }
                      />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </section>

        <section className="flex shrink-0 flex-col gap-3">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-medium">
              {t("chatgptOAuthRouting.sequenceTitle")}
            </h3>
            <Badge variant="outline">
              {t("chatgptOAuthRouting.recentRequestsCount", {
                count: recentRequests.length,
              })}
            </Badge>
          </div>

          {loading ? (
            <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.loadingEvidence")}
            </div>
          ) : recentRequests.length > 0 ? (
            <div className="pr-1">
              <div className="space-y-2">
                {recentRequests.map((request) => (
                  <div
                    key={request.span_id}
                    className="rounded-lg border bg-muted/10 px-3 py-2.5"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0 space-y-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <Badge
                            variant={request.used_failover ? "warning" : "info"}
                          >
                            {request.used_failover
                              ? t("chatgptOAuthRouting.monitorFailoverLabel")
                              : t("chatgptOAuthRouting.monitorDirectLabel")}
                          </Badge>
                          {request.used_failover && request.selected_provider ? (
                            <>
                              <Badge variant="outline">
                                {t("chatgptOAuthRouting.selectedProviderBadge", {
                                  provider: request.selected_provider,
                                })}
                              </Badge>
                              <Badge variant="warning">
                                {t("chatgptOAuthRouting.servedProviderBadge", {
                                  provider: request.provider_name,
                                })}
                              </Badge>
                            </>
                          ) : (
                            <Badge variant="outline">
                              {request.provider_name}
                            </Badge>
                          )}
                        </div>
                        <p className="truncate text-sm font-medium">
                          {request.model || t("chatgptOAuthRouting.unknownModel")}
                        </p>
                        <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                          <span>{formatRelativeTime(request.started_at)}</span>
                          <span>{formatDuration(request.duration_ms)}</span>
                          <span>
                            {t("chatgptOAuthRouting.attemptCount", {
                              count: request.attempt_count,
                            })}
                          </span>
                        </div>
                      </div>

                      <div className="flex items-center gap-2">
                        <Badge variant={requestStatusVariant(request.status)}>
                          {request.status}
                        </Badge>
                        <Button
                          asChild
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 shrink-0"
                        >
                          <Link
                            to={`/traces/${request.trace_id}`}
                            aria-label={t("chatgptOAuthRouting.openTrace")}
                            title={t("chatgptOAuthRouting.openTrace")}
                          >
                            <ArrowUpRight className="h-4 w-4" />
                          </Link>
                        </Button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className="rounded-lg border border-dashed bg-muted/5">
              <EmptyState
                icon={Route}
                title={t("chatgptOAuthRouting.sequenceEmptyTitle")}
                description={t("chatgptOAuthRouting.noEvidence")}
                className="py-6"
              />
            </div>
          )}
        </section>
      </CardContent>
    </Card>
  );
}
