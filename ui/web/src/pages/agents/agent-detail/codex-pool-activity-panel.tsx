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
  successCount: number;
  failureCount: number;
  consecutiveFailures: number;
  successRate: number;
  healthScore: number;
  healthState: "healthy" | "degraded" | "critical" | "idle";
  lastSelectedAt?: string;
  lastFailoverAt?: string;
  lastUsedAt?: string;
  lastSuccessAt?: string;
  lastFailureAt?: string;
  providerHref?: string;
  quota?: ChatGPTOAuthProviderQuota | null;
}

interface CodexPoolActivityPanelProps {
  entries: CodexPoolEntry[];
  strategy: "manual" | "round_robin";
  recentRequests: CodexPoolRecentRequest[];
  statsSampleSize: number;
  fetching: boolean;
  showProviderLinks?: boolean;
  onRefresh: () => void;
  className?: string;
}

interface CodexPoolRecentRequestsListProps {
  recentRequests: CodexPoolRecentRequest[];
  loading: boolean;
  compact?: boolean;
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

function runtimeHealthVariant(
  state: CodexPoolEntry["healthState"],
): "success" | "warning" | "destructive" | "outline" {
  if (state === "healthy") return "success";
  if (state === "degraded") return "warning";
  if (state === "critical") return "destructive";
  return "outline";
}

function runtimeHealthBarWidths(entry: CodexPoolEntry) {
  const total = entry.successCount + entry.failureCount;
  if (total <= 0) {
    return { success: 0, failure: 0 };
  }
  const success = Math.max(0, Math.min(100, entry.successRate));
  return {
    success,
    failure: Math.max(0, 100 - success),
  };
}

function poolRoleBadgeClass(role: CodexPoolEntry["role"]): string {
  if (role === "preferred") {
    return "border-primary/35 bg-primary/12 text-foreground shadow-sm dark:border-primary/40 dark:bg-primary/18";
  }
  return "border-border/70 bg-background/80 text-muted-foreground";
}

function MonitorStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="h-full rounded-lg border bg-background/70 px-2.5 py-2">
      <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </p>
      <p className="mt-1 text-sm font-semibold leading-tight tabular-nums">
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
      <p className="mt-1 text-sm font-semibold leading-tight tabular-nums">
        {value}
      </p>
    </div>
  );
}

function requestProviderSummary(request: CodexPoolRecentRequest): string {
  const selected = request.selected_provider?.trim();
  const served = request.provider_name?.trim();
  if (request.used_failover && selected && served && selected !== served) {
    return `${selected} -> ${served}`;
  }
  return served || selected || "";
}

interface RequestAccentClasses {
  card: string;
  glow: string;
  stripe: string;
  marker: string;
  index: string;
  directBadge: string;
  pill: string;
  trace: string;
}

const REQUEST_ACCENTS: RequestAccentClasses[] = [
  {
    card: "border-emerald-500/20",
    glow:
      "from-emerald-500/[0.12] via-emerald-500/[0.04] to-transparent dark:from-emerald-500/[0.16] dark:via-emerald-500/[0.06]",
    stripe: "from-emerald-400/90 via-teal-400/75 to-cyan-400/35",
    marker: "bg-emerald-500",
    index:
      "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-200",
    directBadge:
      "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200",
    pill:
      "border-emerald-500/15 bg-emerald-500/[0.07] text-emerald-950 dark:border-emerald-500/20 dark:bg-emerald-500/[0.12] dark:text-emerald-100",
    trace:
      "text-emerald-700 hover:border-emerald-500/25 hover:bg-emerald-500/10 hover:text-emerald-800 dark:text-emerald-200 dark:hover:border-emerald-500/30 dark:hover:bg-emerald-500/15 dark:hover:text-emerald-100",
  },
  {
    card: "border-sky-500/20",
    glow:
      "from-sky-500/[0.12] via-sky-500/[0.04] to-transparent dark:from-sky-500/[0.16] dark:via-sky-500/[0.06]",
    stripe: "from-sky-400/90 via-cyan-400/75 to-blue-400/35",
    marker: "bg-sky-500",
    index:
      "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-200",
    directBadge:
      "border-sky-500/20 bg-sky-500/10 text-sky-700 dark:border-sky-500/25 dark:bg-sky-500/15 dark:text-sky-200",
    pill:
      "border-sky-500/15 bg-sky-500/[0.07] text-sky-950 dark:border-sky-500/20 dark:bg-sky-500/[0.12] dark:text-sky-100",
    trace:
      "text-sky-700 hover:border-sky-500/25 hover:bg-sky-500/10 hover:text-sky-800 dark:text-sky-200 dark:hover:border-sky-500/30 dark:hover:bg-sky-500/15 dark:hover:text-sky-100",
  },
  {
    card: "border-amber-500/20",
    glow:
      "from-amber-500/[0.12] via-amber-500/[0.04] to-transparent dark:from-amber-500/[0.16] dark:via-amber-500/[0.06]",
    stripe: "from-amber-400/90 via-orange-400/75 to-yellow-400/35",
    marker: "bg-amber-500",
    index:
      "border-amber-500/30 bg-amber-500/10 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/15 dark:text-amber-100",
    directBadge:
      "border-amber-500/20 bg-amber-500/10 text-amber-800 dark:border-amber-500/25 dark:bg-amber-500/15 dark:text-amber-100",
    pill:
      "border-amber-500/15 bg-amber-500/[0.07] text-amber-950 dark:border-amber-500/20 dark:bg-amber-500/[0.12] dark:text-amber-50",
    trace:
      "text-amber-800 hover:border-amber-500/25 hover:bg-amber-500/10 hover:text-amber-900 dark:text-amber-100 dark:hover:border-amber-500/30 dark:hover:bg-amber-500/15 dark:hover:text-amber-50",
  },
  {
    card: "border-rose-500/20",
    glow:
      "from-rose-500/[0.12] via-rose-500/[0.04] to-transparent dark:from-rose-500/[0.16] dark:via-rose-500/[0.06]",
    stripe: "from-rose-400/90 via-pink-400/75 to-orange-300/35",
    marker: "bg-rose-500",
    index:
      "border-rose-500/30 bg-rose-500/10 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-200",
    directBadge:
      "border-rose-500/20 bg-rose-500/10 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/15 dark:text-rose-200",
    pill:
      "border-rose-500/15 bg-rose-500/[0.07] text-rose-950 dark:border-rose-500/20 dark:bg-rose-500/[0.12] dark:text-rose-100",
    trace:
      "text-rose-700 hover:border-rose-500/25 hover:bg-rose-500/10 hover:text-rose-800 dark:text-rose-200 dark:hover:border-rose-500/30 dark:hover:bg-rose-500/15 dark:hover:text-rose-100",
  },
];

function requestAccentSeed(request: CodexPoolRecentRequest): string {
  return (
    request.provider_name?.trim() ||
    request.selected_provider?.trim() ||
    requestProviderSummary(request) ||
    request.model ||
    request.span_id
  );
}

function requestAccentClasses(seed: string): RequestAccentClasses {
  let hash = 0;
  for (const char of seed) {
    hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  }
  return REQUEST_ACCENTS[hash % REQUEST_ACCENTS.length] ?? REQUEST_ACCENTS[0]!;
}

function CodexPoolRecentRequestsList({
  recentRequests,
  loading,
  compact = false,
  className,
}: CodexPoolRecentRequestsListProps) {
  const { t } = useTranslation("agents");

  if (loading) {
    return (
      <div
        className={cn(
          "rounded-lg border border-dashed text-muted-foreground",
          compact ? "px-3 py-3 text-xs" : "px-4 py-4 text-sm",
          className,
        )}
      >
        {t("chatgptOAuthRouting.loadingEvidence")}
      </div>
    );
  }

  if (recentRequests.length === 0) {
    return compact ? (
      <div
        className={cn(
          "rounded-lg border border-dashed bg-muted/5 px-3 py-3 text-xs text-muted-foreground",
          className,
        )}
      >
        {t("chatgptOAuthRouting.noEvidence")}
      </div>
    ) : (
      <div className={cn("rounded-lg border border-dashed bg-muted/5", className)}>
        <EmptyState
          icon={Route}
          title={t("chatgptOAuthRouting.sequenceEmptyTitle")}
          description={t("chatgptOAuthRouting.noEvidence")}
          className="py-6"
        />
      </div>
    );
  }

  if (compact) {
    return (
      <div
        className={cn(
          "overflow-x-auto overflow-y-hidden overscroll-contain pb-1",
          className,
        )}
      >
        <div className="flex min-w-max gap-2">
          {recentRequests.map((request, index) => {
            const providerSummary =
              requestProviderSummary(request) ||
              request.model ||
              t("chatgptOAuthRouting.unknownModel");
            const accent = requestAccentClasses(requestAccentSeed(request));

            return (
              <div
                key={request.span_id}
                className={cn(
                  "relative isolate flex min-h-[6.5rem] w-[15.5rem] shrink-0 snap-start flex-col overflow-hidden rounded-lg border bg-background/80 p-2.5",
                  "transition-colors hover:bg-background",
                  accent.card,
                )}
              >
                <div
                  aria-hidden
                  className={cn(
                    "pointer-events-none absolute inset-x-0 top-0 h-1 bg-gradient-to-r",
                    accent.stripe,
                  )}
                />
                <div
                  aria-hidden
                  className={cn(
                    "pointer-events-none absolute inset-0 bg-gradient-to-br",
                    accent.glow,
                  )}
                />

                <div className="relative z-10 flex items-start justify-between gap-2">
                  <div className="flex min-w-0 items-start gap-2">
                    <div
                      className={cn(
                        "flex h-7 w-7 shrink-0 items-center justify-center rounded-full border text-[11px] font-semibold tabular-nums",
                        accent.index,
                      )}
                    >
                      {index + 1}
                    </div>

                    <div className="min-w-0">
                      <div className="flex min-w-0 items-center gap-1.5">
                        <span
                          aria-hidden
                          className={cn(
                            "h-2 w-2 shrink-0 rounded-full",
                            accent.marker,
                          )}
                        />
                        <p className="truncate text-sm font-semibold leading-tight">
                          {providerSummary}
                        </p>
                      </div>
                      <p className="truncate text-[11px] text-muted-foreground">
                        {request.model || t("chatgptOAuthRouting.unknownModel")}
                      </p>
                    </div>
                  </div>

                  <Button
                    asChild
                    variant="ghost"
                    size="icon"
                    className={cn(
                      "h-7 w-7 shrink-0 rounded-full border border-transparent",
                      accent.trace,
                    )}
                  >
                    <Link
                      to={`/traces/${request.trace_id}`}
                      aria-label={t("chatgptOAuthRouting.openTrace")}
                      title={t("chatgptOAuthRouting.openTrace")}
                    >
                      <ArrowUpRight className="h-3.5 w-3.5" />
                    </Link>
                  </Button>
                </div>

                <div className="relative z-10 mt-2 flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
                  <Badge
                    variant={request.used_failover ? "warning" : "outline"}
                    className={request.used_failover ? undefined : accent.directBadge}
                  >
                    {request.used_failover
                      ? t("chatgptOAuthRouting.monitorFailoverLabel")
                      : t("chatgptOAuthRouting.monitorDirectLabel")}
                  </Badge>
                  <span
                    className={cn(
                      "inline-flex items-center rounded-full border px-2 py-0.5 whitespace-nowrap font-medium tabular-nums",
                      accent.pill,
                    )}
                  >
                    {formatRelativeTime(request.started_at)}
                  </span>
                  <span
                    className={cn(
                      "inline-flex items-center rounded-full border px-2 py-0.5 whitespace-nowrap font-medium tabular-nums",
                      accent.pill,
                    )}
                  >
                    {formatDuration(request.duration_ms)}
                  </span>
                  <span
                    className={cn(
                      "inline-flex items-center rounded-full border px-2 py-0.5 whitespace-nowrap",
                      accent.pill,
                    )}
                  >
                    {t("chatgptOAuthRouting.attemptCount", {
                      count: request.attempt_count,
                    })}
                  </span>
                  {request.status !== "completed" && (
                    <Badge variant={requestStatusVariant(request.status)}>
                      {request.status}
                    </Badge>
                  )}
                </div>

                {request.used_failover &&
                  request.failover_providers &&
                  request.failover_providers.length > 0 && (
                    <p className="relative z-10 mt-1 truncate text-[11px] text-muted-foreground">
                      {t("chatgptOAuthRouting.failoverHint", {
                        providers: request.failover_providers.join(", "),
                      })}
                    </p>
                  )}
              </div>
            );
          })}
        </div>
      </div>
    );
  }

  return (
    <div className={cn("min-h-0 flex-1 overflow-y-auto overscroll-contain pr-1", className)}>
      <div className="space-y-2">
        {recentRequests.map((request) => {
          const providerSummary = requestProviderSummary(request);
          return (
            <div
              key={request.span_id}
              className={cn(
                "rounded-lg border bg-muted/10",
                "px-3 py-2.5",
              )}
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 space-y-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant={request.used_failover ? "warning" : "info"}>
                      {request.used_failover
                        ? t("chatgptOAuthRouting.monitorFailoverLabel")
                        : t("chatgptOAuthRouting.monitorDirectLabel")}
                    </Badge>
                    {providerSummary ? (
                      <Badge variant="outline">{providerSummary}</Badge>
                    ) : (
                      <Badge variant="secondary">{request.status}</Badge>
                    )}
                  </div>

                  <p className="truncate text-sm font-medium">
                    {request.model || t("chatgptOAuthRouting.unknownModel")}
                  </p>

                  <div
                    className={cn(
                      "flex flex-wrap gap-x-3 gap-y-1 text-muted-foreground",
                      "text-xs",
                    )}
                  >
                    <span>{formatRelativeTime(request.started_at)}</span>
                    <span>{formatDuration(request.duration_ms)}</span>
                    <span>
                      {t("chatgptOAuthRouting.attemptCount", {
                        count: request.attempt_count,
                      })}
                    </span>
                  </div>

                  {request.used_failover &&
                    request.failover_providers &&
                    request.failover_providers.length > 0 && (
                      <p
                        className={cn(
                          "text-muted-foreground",
                          "text-xs",
                        )}
                      >
                        {t("chatgptOAuthRouting.failoverHint", {
                          providers: request.failover_providers.join(", "),
                        })}
                      </p>
                    )}
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
          );
        })}
      </div>
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
        failureKind: getQuotaFailureKind(entry.quota),
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
      <CardHeader className="border-b bg-muted/20 px-4 py-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle className="text-base">
              {t("chatgptOAuthRouting.activityTitle")}
            </CardTitle>
            <p className="text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.activityDescription")}
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline">
              {strategy === "round_robin"
                ? t("chatgptOAuthRouting.strategy.roundRobin")
                : t("chatgptOAuthRouting.strategy.manual")}
            </Badge>
            {blockedEntries.length > 0 && (
              <Badge variant="warning">
                {t("chatgptOAuthRouting.blockedNowTitle")} {blockedEntries.length}
              </Badge>
            )}
            {failoverOnlyProviders > 0 && (
              <Badge variant="warning">
                {t("chatgptOAuthRouting.failoverOnlyProviders", {
                  count: failoverOnlyProviders,
                })}
              </Badge>
            )}
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
        </div>
      </CardHeader>

      <CardContent className="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden px-4 py-3">
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
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
            value={String(
              recentRequests.filter((request) => request.used_failover)
                .length,
            )}
          />
        </div>

        <section className="shrink-0 rounded-lg border bg-muted/5 p-2.5">
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

          <CodexPoolRecentRequestsList
            recentRequests={recentRequests}
            loading={fetching && recentRequests.length === 0}
            compact
            className="mt-2"
          />
        </section>

        <section className="flex min-h-0 flex-1 flex-col gap-3">
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
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain pr-1">
              <div className="grid auto-rows-min content-start gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(100%,15rem),1fr))]">
                {routeEntries.map((entry) => {
                  const accent = requestAccentClasses(entry.name);
                  return (
                    <div
                      key={entry.name}
                      className={cn(
                        "relative isolate overflow-hidden rounded-lg border bg-background/80 p-2.5",
                        accent.card,
                      )}
                    >
                      <div
                        aria-hidden
                        className={cn(
                          "pointer-events-none absolute inset-x-0 top-0 h-1 bg-gradient-to-r",
                          accent.stripe,
                        )}
                      />
                      <div
                        aria-hidden
                        className={cn(
                          "pointer-events-none absolute inset-0 bg-gradient-to-br",
                          accent.glow,
                        )}
                      />

                      <div className="relative z-10">
                        {(() => {
                          const totalOutcomes =
                            entry.successCount + entry.failureCount;
                          const barWidths = runtimeHealthBarWidths(entry);
                          return (
                            <>
                              <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0 space-y-1">
                                  <div className="flex flex-wrap items-center gap-2">
                                    <span className="inline-flex min-w-0 items-center gap-1.5 truncate text-sm font-medium">
                                      <span
                                        aria-hidden
                                        className={cn(
                                          "h-2 w-2 shrink-0 rounded-full",
                                          accent.marker,
                                        )}
                                      />
                                      <span className="truncate">{entry.label}</span>
                                    </span>
                                    <Badge
                                      variant="outline"
                                      className={poolRoleBadgeClass(entry.role)}
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
                                      variant={runtimeHealthVariant(entry.healthState)}
                                    >
                                      {t(
                                        `chatgptOAuthRouting.healthState.${entry.healthState}`,
                                      )}
                                    </Badge>
                                    {entry.routeReadiness !== "healthy" && (
                                      <Badge
                                        variant={routeBadgeVariant(entry.routeReadiness)}
                                      >
                                        {t(routeLabelKey(entry.routeReadiness))}
                                      </Badge>
                                    )}
                                  </div>
                                  {entry.label !== entry.name && (
                                    <p className="truncate font-mono text-xs text-muted-foreground">
                                      {entry.name}
                                    </p>
                                  )}
                                </div>

                                {showProviderLinks && entry.providerHref && (
                                  <Button
                                    asChild
                                    variant="ghost"
                                    size="icon"
                                    className={cn("h-8 w-8 shrink-0 rounded-full", accent.trace)}
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

                              <div className="mt-2 rounded-md border bg-background/75 px-2.5 py-1.5">
                                <div className="flex flex-wrap items-start justify-between gap-2">
                                  <div className="min-w-0">
                                    <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                                      {t("chatgptOAuthRouting.runtimeHealthTitle")}
                                    </p>
                                    {totalOutcomes > 0 ? (
                                      <p className="mt-1 text-xs text-muted-foreground">
                                        {t(
                                          "chatgptOAuthRouting.runtimeHealthSummary",
                                          {
                                            rate: entry.successRate,
                                            score: entry.healthScore,
                                          },
                                        )}
                                      </p>
                                    ) : (
                                      <p className="mt-1 text-xs text-muted-foreground">
                                        {t("chatgptOAuthRouting.noRuntimeSample")}
                                      </p>
                                    )}
                                  </div>
                                  {entry.consecutiveFailures > 0 && (
                                    <Badge
                                      variant={
                                        entry.consecutiveFailures >= 3
                                          ? "destructive"
                                          : "warning"
                                      }
                                    >
                                      {t("chatgptOAuthRouting.failureStreakBadge", {
                                        count: entry.consecutiveFailures,
                                      })}
                                    </Badge>
                                  )}
                                </div>

                                <div className="mt-2 flex h-2 overflow-hidden rounded-full bg-muted">
                                  {totalOutcomes > 0 ? (
                                    <>
                                      <div
                                        className="h-full bg-emerald-500 transition-all"
                                        style={{ width: `${barWidths.success}%` }}
                                      />
                                      {barWidths.failure > 0 && (
                                        <div
                                          className="h-full bg-rose-500/80 transition-all"
                                          style={{ width: `${barWidths.failure}%` }}
                                        />
                                      )}
                                    </>
                                  ) : (
                                    <div className="h-full w-full bg-muted" />
                                  )}
                                </div>

                                <div className="mt-2 flex items-center justify-between gap-3 text-[11px] text-muted-foreground">
                                  <span>
                                    {t("chatgptOAuthRouting.runtimeSuccessCompact", {
                                      count: entry.successCount,
                                    })}
                                  </span>
                                  <span>
                                    {t("chatgptOAuthRouting.runtimeFailureCompact", {
                                      count: entry.failureCount,
                                    })}
                                  </span>
                                </div>

                                {(entry.lastSuccessAt || entry.lastFailureAt) && (
                                  <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
                                    {entry.lastSuccessAt && (
                                      <span>
                                        {t("chatgptOAuthRouting.lastSuccessLabel", {
                                          value: formatRelativeTime(entry.lastSuccessAt),
                                        })}
                                      </span>
                                    )}
                                    {entry.lastFailureAt && (
                                      <span>
                                        {t("chatgptOAuthRouting.lastFailureLabel", {
                                          value: formatRelativeTime(entry.lastFailureAt),
                                        })}
                                      </span>
                                    )}
                                  </div>
                                )}
                              </div>

                              <ChatGPTOAuthQuotaStrip
                                quota={entry.quota}
                                className="mt-2"
                                compact
                              />

                              <div className="mt-2 grid gap-2 sm:grid-cols-3">
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
                            </>
                          );
                        })()}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </section>
      </CardContent>
    </Card>
  );
}

interface CodexPoolRecentRequestsPanelProps {
  recentRequests: CodexPoolRecentRequest[];
  loading: boolean;
  className?: string;
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
        <CodexPoolRecentRequestsList
          recentRequests={recentRequests}
          loading={loading}
        />
      </CardContent>
    </Card>
  );
}
