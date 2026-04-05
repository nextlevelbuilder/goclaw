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

interface ChatGPTOAuthQuotaBadgesProps {
  quota?: ChatGPTOAuthProviderQuota | null;
  loading?: boolean;
  translationNamespace?: "agents" | "providers";
  translationKeyPrefix?: string;
  className?: string;
}


export function ChatGPTOAuthQuotaBadges({
  quota,
  loading = false,
  translationNamespace = "agents",
  translationKeyPrefix = "chatgptOAuthRouting.quota",
  className,
}: ChatGPTOAuthQuotaBadgesProps) {
  const { t } = useTranslation(translationNamespace);

  if (loading && !quota) {
    return (
      <Badge variant="outline">{t(`${translationKeyPrefix}.checking`)}</Badge>
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
                {t(`${translationKeyPrefix}.failure.${failureKind}.label`)}
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
                {t(`${translationKeyPrefix}.failure.${failureKind}.label`)}
              </p>
              <p className="text-muted-foreground">
                {t(`${translationKeyPrefix}.failure.${failureKind}.description`)}
              </p>
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
