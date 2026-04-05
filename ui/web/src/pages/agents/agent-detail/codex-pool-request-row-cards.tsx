import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowUpRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatDuration, formatRelativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { CodexPoolRecentRequest } from "./hooks/use-codex-pool-activity";
import {
  requestAccentClasses,
  requestAccentSeed,
  requestProviderSummary,
} from "./codex-pool-request-accent";

export function requestStatusVariant(
  status: string,
): "success" | "destructive" | "info" | "secondary" {
  if (status === "ok" || status === "success" || status === "completed")
    return "success";
  if (status === "error" || status === "failed") return "destructive";
  if (status === "running" || status === "pending") return "info";
  return "secondary";
}

interface CompactRequestCardProps {
  request: CodexPoolRecentRequest;
  index: number;
}

export function CompactRequestCard({ request, index }: CompactRequestCardProps) {
  const { t } = useTranslation("agents");
  const providerSummary =
    requestProviderSummary(request) ||
    request.model ||
    t("chatgptOAuthRouting.unknownModel");
  const accent = requestAccentClasses(requestAccentSeed(request));

  return (
    <div
      className={cn(
        "relative isolate flex min-h-[4.85rem] w-[12.75rem] shrink-0 snap-start flex-col overflow-hidden rounded-lg border bg-background/80 p-2.5",
        "lg:min-h-[5.35rem] lg:w-[13.75rem] xl:min-h-[5.85rem] xl:w-[14.75rem] sm:xl:w-[15rem]",
        "[@media(max-height:760px)]:min-h-[4.35rem] [@media(max-height:760px)]:w-[11.75rem] [@media(max-height:760px)]:p-1.5",
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

      <Button
        asChild
        variant="ghost"
        size="icon"
        className={cn(
          "absolute right-1.5 top-1.5 z-10 h-5 w-5 shrink-0 rounded-full border border-transparent xl:right-2 xl:top-2 xl:h-6 xl:w-6",
          accent.trace,
        )}
      >
        <Link
          to={`/traces/${request.trace_id}`}
          aria-label={t("chatgptOAuthRouting.openTrace")}
          title={t("chatgptOAuthRouting.openTrace")}
        >
          <ArrowUpRight className="h-2.5 w-2.5 xl:h-3 xl:w-3" />
        </Link>
      </Button>

      <div className="relative z-10 flex min-w-0 items-start gap-2 pr-6 xl:pr-7">
        <div className="flex min-w-0 flex-1 items-start gap-2">
          <div
            className={cn(
              "flex h-5 w-5 shrink-0 items-center justify-center rounded-full border text-[9px] font-semibold tabular-nums",
              "xl:h-6 xl:w-6 xl:text-[10px]",
              accent.index,
            )}
          >
            {index + 1}
          </div>
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-1.5">
              <span
                aria-hidden
                className={cn("h-2 w-2 shrink-0 rounded-full", accent.marker)}
              />
              <p className="truncate text-[13px] font-semibold leading-tight xl:text-sm">
                {providerSummary}
              </p>
            </div>
            <p className="truncate text-[10px] text-muted-foreground xl:text-[11px]">
              {request.model || t("chatgptOAuthRouting.unknownModel")}
            </p>
          </div>
        </div>
      </div>

      <div className="relative z-10 mt-1 flex items-center gap-1.5 overflow-hidden whitespace-nowrap text-[10px] text-muted-foreground xl:mt-2 xl:gap-2 xl:text-[11px]">
        <Badge
          variant={request.used_failover ? "warning" : "outline"}
          className={cn(
            "h-5 shrink-0 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs",
            request.used_failover ? undefined : accent.directBadge,
          )}
        >
          {request.used_failover
            ? t("chatgptOAuthRouting.monitorFailoverLabel")
            : t("chatgptOAuthRouting.monitorDirectLabel")}
        </Badge>
        <span className="shrink-0 font-medium tabular-nums">
          {formatRelativeTime(request.started_at)}
        </span>
        <span aria-hidden className="shrink-0 text-muted-foreground/70">·</span>
        <span className="shrink-0 font-medium tabular-nums">
          {formatDuration(request.duration_ms)}
        </span>
        {request.attempt_count > 1 && (
          <>
            <span aria-hidden className="shrink-0 text-muted-foreground/70">·</span>
            <span className="shrink-0 font-medium tabular-nums">
              {request.attempt_count}x
            </span>
          </>
        )}
        {request.status !== "completed" && (
          <Badge
            variant={requestStatusVariant(request.status)}
            className="h-5 shrink-0 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs"
          >
            {request.status}
          </Badge>
        )}
      </div>

      {request.used_failover &&
        request.failover_providers &&
        request.failover_providers.length > 0 && (
          <p className="relative z-10 mt-0.5 truncate text-[10px] text-muted-foreground xl:text-[11px]">
            {t("chatgptOAuthRouting.failoverHint", {
              providers: request.failover_providers.join(", "),
            })}
          </p>
        )}
    </div>
  );
}

export function FullRequestRow({ request }: { request: CodexPoolRecentRequest }) {
  const { t } = useTranslation("agents");
  const providerSummary = requestProviderSummary(request);

  return (
    <div className="rounded-lg border bg-muted/10 px-3 py-2.5">
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

          <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>{formatRelativeTime(request.started_at)}</span>
            <span>{formatDuration(request.duration_ms)}</span>
            <span>
              {t("chatgptOAuthRouting.attemptCount", { count: request.attempt_count })}
            </span>
          </div>

          {request.used_failover &&
            request.failover_providers &&
            request.failover_providers.length > 0 && (
              <p className="text-xs text-muted-foreground">
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
          <Button asChild variant="ghost" size="icon" className="h-8 w-8 shrink-0">
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
}
