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
import {
  getQuotaBadgeVariant,
  getQuotaFailureKind,
  getQuotaPlanLabel,
  getQuotaSignals,
} from "./chatgpt-oauth-quota-utils";

interface ChatGPTOAuthQuotaStripProps {
  quota?: ChatGPTOAuthProviderQuota | null;
  className?: string;
}

const failureVariantByKind = {
  billing: "destructive",
  exhausted: "destructive",
  reauth: "warning",
  forbidden: "destructive",
  needs_setup: "warning",
  retry_later: "outline",
  unavailable: "outline",
} as const;

function quotaBarClass(remaining: number): string {
  if (remaining <= 20) return "bg-destructive";
  if (remaining <= 50) return "bg-amber-500";
  return "bg-emerald-500";
}

export function ChatGPTOAuthQuotaStrip({
  quota,
  className,
}: ChatGPTOAuthQuotaStripProps) {
  const { t } = useTranslation("agents");

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
              "space-y-1.5 rounded-md border bg-background/70 px-2.5 py-2",
              className,
            )}
          >
            <div className="flex flex-wrap items-center gap-1.5">
              {failureKind ? (
                <Badge variant={failureVariantByKind[failureKind]}>
                  {t(`chatgptOAuthRouting.quota.failure.${failureKind}.label`)}
                </Badge>
              ) : (
                <>
                  {planLabel && <Badge variant="outline">{planLabel}</Badge>}
                  {signals.map((signal) => (
                    <Badge
                      key={signal.shortLabel}
                      variant={getQuotaBadgeVariant(signal.remaining)}
                    >
                      {signal.shortLabel} {signal.remaining}%
                    </Badge>
                  ))}
                </>
              )}
            </div>

            {!failureKind && signals.length > 0 && (
              <div className="grid gap-1">
                {signals.map((signal) => (
                  <div
                    key={signal.shortLabel}
                    className="flex items-center gap-2"
                  >
                    <span className="w-7 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                      {signal.shortLabel}
                    </span>
                    <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
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
                    <span className="w-10 text-right text-[11px] font-medium">
                      {signal.remaining}%
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </TooltipTrigger>

        <TooltipContent sideOffset={6} className="max-w-72 px-3 py-2">
          {failureKind ? (
            <div className="space-y-1.5">
              <p className="font-medium">
                {t(`chatgptOAuthRouting.quota.failure.${failureKind}.label`)}
              </p>
              <p className="text-muted-foreground">
                {t(
                  `chatgptOAuthRouting.quota.failure.${failureKind}.description`,
                )}
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
                    {t("chatgptOAuthRouting.quota.plan")}
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
                {t("chatgptOAuthRouting.quota.lastChecked", {
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
