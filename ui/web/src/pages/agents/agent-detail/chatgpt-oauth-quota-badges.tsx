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

interface ChatGPTOAuthQuotaBadgesProps {
  quota?: ChatGPTOAuthProviderQuota | null;
  loading?: boolean;
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

export function ChatGPTOAuthQuotaBadges({
  quota,
  loading = false,
  className,
}: ChatGPTOAuthQuotaBadgesProps) {
  const { t } = useTranslation("agents");

  if (loading && !quota) {
    return (
      <Badge variant="outline">{t("chatgptOAuthRouting.quota.checking")}</Badge>
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
          <div className={cn("flex flex-wrap items-center gap-1.5", className)}>
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
        </TooltipTrigger>

        <TooltipContent sideOffset={6} className="max-w-64 px-3 py-2">
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
