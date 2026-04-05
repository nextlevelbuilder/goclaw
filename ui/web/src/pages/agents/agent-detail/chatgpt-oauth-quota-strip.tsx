import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { formatDate, formatRelativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { ChatGPTOAuthProviderQuota } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import { failureVariantByKind } from "./agent-display-utils";
import {
  getQuotaBadgeVariant,
  getQuotaFailureKind,
  getQuotaPlanLabel,
  getQuotaSignals,
} from "./chatgpt-oauth-quota-utils";

interface ChatGPTOAuthQuotaStripProps {
  quota?: ChatGPTOAuthProviderQuota | null;
  loading?: boolean;
  translationNamespace?: "agents" | "providers";
  translationKeyPrefix?: string;
  className?: string;
  compact?: boolean;
  layout?: "stacked" | "inline";
  embedded?: boolean;
  showSignalBadges?: boolean;
}

function quotaBarClass(remaining: number): string {
  if (remaining <= 20) return "bg-destructive";
  if (remaining <= 50) return "bg-amber-500";
  return "bg-emerald-500";
}

export function ChatGPTOAuthQuotaStrip({
  quota,
  loading = false,
  translationNamespace = "agents",
  translationKeyPrefix = "chatgptOAuthRouting.quota",
  className,
  compact = false,
  layout = "stacked",
  embedded = false,
  showSignalBadges = !compact,
}: ChatGPTOAuthQuotaStripProps) {
  const { t } = useTranslation(translationNamespace);

  if (loading && !quota) {
    return (
      <Badge variant="outline" className={cn("h-5 px-1.5 text-[10px]", className)}>
        {t(`${translationKeyPrefix}.checking`)}
      </Badge>
    );
  }

  if (!quota) return null;

  const failureKind = getQuotaFailureKind(quota);
  const signals = getQuotaSignals(quota);
  const planLabel = getQuotaPlanLabel(quota.plan_type);

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div
            className={cn(
              layout === "inline"
                ? "flex min-w-0 items-center gap-1.5"
                : compact
                  ? cn(
                      "space-y-1 rounded-md px-2.5 py-1.5",
                      embedded ? "bg-transparent px-0 py-0" : "border bg-background/70",
                    )
                  : cn(
                      "space-y-1.5 rounded-md px-2.5 py-2",
                      embedded ? "bg-transparent px-0 py-0" : "border bg-background/70",
                    ),
              className,
            )}
          >
            {failureKind ? (
              <Badge
                variant={failureVariantByKind[failureKind]}
                className="h-5 px-1.5 text-[10px]"
              >
                {t(`${translationKeyPrefix}.failure.${failureKind}.label`)}
              </Badge>
            ) : layout === "inline" ? (
              <>
                {planLabel && (
                  <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                    {planLabel}
                  </Badge>
                )}
                {signals.map((signal) => (
                  <div
                    key={signal.shortLabel}
                    className="flex items-center gap-1 rounded-full border bg-background/70 px-1.5 py-1"
                  >
                    <span className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground">
                      {signal.shortLabel}
                    </span>
                    <div className="h-1.5 w-11 overflow-hidden rounded-full bg-muted">
                      <div
                        className={cn(
                          "h-full rounded-full transition-all",
                          quotaBarClass(signal.remaining),
                        )}
                        style={{
                          width: `${Math.max(6, Math.min(100, signal.remaining))}%`,
                        }}
                      />
                    </div>
                  </div>
                ))}
              </>
            ) : (
              <>
                <div className="flex flex-wrap items-center gap-1.5">
                  {planLabel && <Badge variant="outline">{planLabel}</Badge>}
                  {showSignalBadges &&
                    signals.map((signal) => (
                      <Badge
                        key={signal.shortLabel}
                        variant={getQuotaBadgeVariant(signal.remaining)}
                      >
                        {signal.shortLabel} {signal.remaining}%
                      </Badge>
                    ))}
                </div>

                {signals.length > 0 && (
                  <div className="grid gap-1">
                    {signals.map((signal) => (
                      <div
                        key={signal.shortLabel}
                        className={cn(
                          "flex items-center gap-2",
                          compact && "gap-1.5",
                        )}
                      >
                        <span className="w-7 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                          {signal.shortLabel}
                        </span>
                        <div className="flex-1 overflow-hidden rounded-full bg-muted h-1.5">
                          <div
                            className={cn(
                              "h-full rounded-full transition-all",
                              quotaBarClass(signal.remaining),
                            )}
                            style={{
                              width: `${Math.max(6, Math.min(100, signal.remaining))}%`,
                            }}
                          />
                        </div>
                        {!compact && (
                          <span className="w-10 text-right text-[11px] font-medium">
                            {signal.remaining}%
                          </span>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}
          </div>
        </TooltipTrigger>

        <TooltipContent sideOffset={6} className="max-w-72 px-3 py-2">
          {failureKind ? (
            <div className="space-y-1.5">
              <p className="font-medium">
                {t(`${translationKeyPrefix}.failure.${failureKind}.label`)}
              </p>
              <p className="text-muted-foreground">
                {t(`${translationKeyPrefix}.failure.${failureKind}.description`)}
              </p>
              {quota.action_hint && (
                <p className="text-muted-foreground">{quota.action_hint}</p>
              )}
            </div>
          ) : (
            <div className="space-y-1.5">
              {planLabel && (
                <div className="flex justify-between gap-3">
                  <span className="text-muted-foreground">
                    {t(`${translationKeyPrefix}.plan`)}
                  </span>
                  <span>{planLabel}</span>
                </div>
              )}
              {signals.map((signal) => (
                <div
                  key={signal.shortLabel}
                  className="flex justify-between gap-3"
                >
                  <span>{signal.shortLabel}</span>
                  <span>
                    {signal.remaining}%
                    {signal.resetAt ? ` · ${formatDate(signal.resetAt)}` : ""}
                  </span>
                </div>
              ))}
              <p className="text-muted-foreground">
                {t(`${translationKeyPrefix}.lastChecked`, {
                  value: formatRelativeTime(quota.last_updated),
                })}
              </p>
            </div>
          )}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
