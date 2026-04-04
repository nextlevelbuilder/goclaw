import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowUpRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatRelativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import {
  routeBadgeVariant,
  routeLabelKey,
} from "./agent-display-utils";
import { ChatGPTOAuthQuotaStrip } from "./chatgpt-oauth-quota-strip";
import { getQuotaFailureKind, getRouteReadiness } from "./chatgpt-oauth-quota-utils";
import type { CodexPoolEntry } from "./codex-pool-entry-types";
import { requestAccentClasses } from "./codex-pool-request-accent";

function availabilityVariant(
  availability: CodexPoolEntry["availability"],
): "success" | "warning" | "outline" {
  if (availability === "ready") return "success";
  if (availability === "needs_sign_in") return "warning";
  return "outline";
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
  if (total <= 0) return { success: 0, failure: 0 };
  const success = Math.max(0, Math.min(100, entry.successRate));
  return { success, failure: Math.max(0, 100 - success) };
}

function poolRoleBadgeClass(role: CodexPoolEntry["role"]): string {
  if (role === "preferred") {
    return "border-primary/35 bg-primary/12 text-foreground shadow-sm dark:border-primary/40 dark:bg-primary/18";
  }
  return "border-border/70 bg-background/80 text-muted-foreground";
}

function MemberMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex h-full items-center justify-between gap-1 rounded-md border bg-background/70 px-2 py-1 xl:px-2.5">
      <p className="truncate text-[9px] font-medium text-muted-foreground xl:text-[10px]">
        {label}
      </p>
      <p className="shrink-0 text-[12px] font-semibold leading-tight tabular-nums xl:text-[13px]">
        {value}
      </p>
    </div>
  );
}

interface CodexPoolMemberCardProps {
  entry: CodexPoolEntry;
  showProviderLinks?: boolean;
}

export function CodexPoolMemberCard({
  entry,
  showProviderLinks = true,
}: CodexPoolMemberCardProps) {
  const { t } = useTranslation("agents");

  const routeReadiness = getRouteReadiness(entry.availability, entry.quota);
  const failureKind = getQuotaFailureKind(entry.quota);
  const accent = requestAccentClasses(entry.name);
  const totalOutcomes = entry.successCount + entry.failureCount;
  const barWidths = runtimeHealthBarWidths(entry);
  const showAvailabilityBadge = entry.availability !== "ready";
  const showHealthBadge =
    entry.healthState !== "healthy" && entry.healthState !== "idle";
  const showRouteBadge = routeReadiness !== "healthy";

  // failureKind used to suppress unused-var lint; quota strip handles display
  void failureKind;

  return (
    <div
      className={cn(
        "relative isolate overflow-hidden rounded-lg border bg-background/80 p-2 lg:min-h-[10.5rem] lg:p-2.5 xl:min-h-[11rem]",
        "[@media(max-height:760px)]:min-h-0 [@media(max-height:760px)]:p-1.5",
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
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0 space-y-1">
            <div className="flex flex-wrap items-center gap-1.5 xl:gap-2">
              <span className="inline-flex min-w-0 items-center gap-1 truncate text-[13px] font-medium xl:gap-1.5 xl:text-sm">
                <span
                  aria-hidden
                  className={cn("h-2 w-2 shrink-0 rounded-full", accent.marker)}
                />
                <span className="truncate">{entry.label}</span>
              </span>
              <Badge
                variant="outline"
                className={cn(
                  "h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs",
                  poolRoleBadgeClass(entry.role),
                )}
              >
                {t(`chatgptOAuthRouting.role.${entry.role}`)}
              </Badge>
              {showAvailabilityBadge && (
                <Badge
                  variant={availabilityVariant(entry.availability)}
                  className="h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs"
                >
                  {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                </Badge>
              )}
              {showHealthBadge && (
                <Badge
                  variant={runtimeHealthVariant(entry.healthState)}
                  className="h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs"
                >
                  {t(`chatgptOAuthRouting.healthState.${entry.healthState}`)}
                </Badge>
              )}
              {showRouteBadge && (
                <Badge
                  variant={routeBadgeVariant(routeReadiness)}
                  className="h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs"
                >
                  {t(routeLabelKey(routeReadiness))}
                </Badge>
              )}
            </div>
            {entry.label !== entry.name && (
              <p className="truncate font-mono text-[10px] text-muted-foreground xl:text-xs">
                {entry.name}
              </p>
            )}
          </div>

          {showProviderLinks && entry.providerHref && (
            <Button
              asChild
              variant="ghost"
              size="icon"
              className={cn(
                "h-7 w-7 shrink-0 rounded-full xl:h-8 xl:w-8",
                accent.trace,
              )}
            >
              <Link
                to={entry.providerHref}
                aria-label={t("chatgptOAuthRouting.openProvider")}
                title={t("chatgptOAuthRouting.openProvider")}
              >
                <ArrowUpRight className="h-3.5 w-3.5 xl:h-4 xl:w-4" />
              </Link>
            </Button>
          )}
        </div>

        <ChatGPTOAuthQuotaStrip
          quota={entry.quota}
          className="mt-1 xl:mt-1.5"
          compact
        />

        <div className="mt-1 rounded-md border bg-background/75 px-2 py-1.5 xl:mt-1.5 xl:px-2.5">
          <div className="flex items-center justify-between gap-2">
            {totalOutcomes > 0 ? (
              <p className="truncate text-[11px] font-medium text-foreground xl:text-xs">
                {t("chatgptOAuthRouting.runtimeHealthSummary", {
                  rate: entry.successRate,
                  score: entry.healthScore,
                })}
              </p>
            ) : (
              <p className="truncate text-[11px] text-muted-foreground xl:text-xs">
                {t("chatgptOAuthRouting.noRuntimeSample")}
              </p>
            )}
            {entry.consecutiveFailures > 0 && (
              <Badge
                variant={entry.consecutiveFailures >= 3 ? "destructive" : "warning"}
                className="h-5 shrink-0 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs"
              >
                {t("chatgptOAuthRouting.failureStreakBadge", {
                  count: entry.consecutiveFailures,
                })}
              </Badge>
            )}
          </div>

          <div className="mt-1 flex h-2 overflow-hidden rounded-full bg-muted xl:mt-1.5">
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

          <div className="mt-1 flex items-center justify-between gap-2 text-[10px] text-muted-foreground xl:mt-1.5 xl:gap-3 xl:text-[11px]">
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
            {entry.lastFailureAt && (
              <span className="truncate">
                {t("chatgptOAuthRouting.lastFailureLabel", {
                  value: formatRelativeTime(entry.lastFailureAt),
                })}
              </span>
            )}
          </div>
        </div>

        <div className="mt-1 grid gap-1 sm:grid-cols-3 xl:mt-1.5 xl:gap-1.5">
          <MemberMetric
            label={t("chatgptOAuthRouting.monitorDirectLabel")}
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
    </div>
  );
}
