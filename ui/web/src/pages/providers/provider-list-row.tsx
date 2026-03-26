import { Cpu, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ChatGPTOAuthQuotaStrip } from "@/pages/agents/agent-detail/chatgpt-oauth-quota-strip";
import type { EffectiveChatGPTOAuthRoutingStrategy } from "@/types/agent";
import type { ChatGPTOAuthProviderQuota } from "./hooks/use-chatgpt-oauth-provider-quotas";
import type { ChatGPTOAuthAvailability } from "./hooks/use-chatgpt-oauth-provider-statuses";
import type { ProviderData } from "./hooks/use-providers";
import { PROVIDER_TYPE_BADGE, ProviderApiKeyBadge } from "./provider-utils";

interface ProviderOAuthPoolSummary {
  availability: ChatGPTOAuthAvailability;
  role: "owner" | "member" | "standalone";
  managedByLabel?: string;
  memberCount: number;
  strategy: EffectiveChatGPTOAuthRoutingStrategy;
  connectorPosition?: "none" | "single" | "first" | "middle" | "last";
  quota?: ChatGPTOAuthProviderQuota | null;
  quotaLoading?: boolean;
}

interface ProviderListRowProps {
  provider: ProviderData;
  oauthPool?: ProviderOAuthPoolSummary;
  onClick: () => void;
  onDelete?: () => void;
}

function strategyLabelKey(strategy: EffectiveChatGPTOAuthRoutingStrategy): string {
  if (strategy === "round_robin") return "list.strategy.roundRobin";
  if (strategy === "priority_order") return "list.strategy.priorityOrder";
  return "list.strategy.primaryFirst";
}

export function ProviderListRow({
  provider,
  oauthPool,
  onClick,
  onDelete,
}: ProviderListRowProps) {
  const { t: tc } = useTranslation("common");
  const { t } = useTranslation("providers");
  const displayName = provider.display_name || provider.name;
  const typeBadge = PROVIDER_TYPE_BADGE[provider.provider_type] ?? {
    label: provider.provider_type,
    variant: "outline" as const,
  };
  const subtitle = provider.provider_type === "chatgpt_oauth"
    ? t("card.oauthAlias", { name: provider.name })
    : provider.display_name
      ? provider.name
      : null;
  const showAvailabilityWarning = oauthPool && oauthPool.availability !== "ready";
  const hasPoolRole = oauthPool?.role === "owner" || oauthPool?.role === "member";
  const showMemberConnector = oauthPool?.role === "member" && oauthPool.connectorPosition && oauthPool.connectorPosition !== "none";
  const poolMeta = oauthPool?.role === "owner"
    ? `${t(strategyLabelKey(oauthPool.strategy))} · ${t("list.memberCount", { count: oauthPool.memberCount })}`
    : oauthPool?.role === "member" && oauthPool.managedByLabel
      ? t("list.managedBy", { provider: oauthPool.managedByLabel })
      : null;
  const secondaryText = [subtitle, poolMeta].filter(Boolean).join(" · ");
  const availabilityWarningLabel = showAvailabilityWarning
    ? t(
        oauthPool?.availability === "disabled"
          ? "list.status.disabled"
          : "list.status.needsSignIn",
      )
    : null;
  const showQuota = provider.provider_type === "chatgpt_oauth"
    && (oauthPool?.quotaLoading || Boolean(oauthPool?.quota));
  const connectorLineClass = oauthPool?.connectorPosition === "first" || oauthPool?.connectorPosition === "middle"
    ? "top-[-0.75rem] h-[calc(100%+1.5rem)]"
    : "top-[-0.75rem] h-[calc(50%+0.75rem)]";

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onClick();
        }
      }}
      className={cn(
        "flex w-full cursor-pointer items-center gap-3 rounded-lg border bg-card px-4 py-2.5 text-left transition-all hover:border-primary/30 hover:shadow-sm",
        oauthPool?.role === "owner" && "border-primary/20 bg-primary/[0.02]",
        oauthPool?.role === "member" && "relative border-sky-500/20 bg-sky-500/[0.025]",
        showMemberConnector && "ml-4 w-[calc(100%-1rem)]",
      )}
    >
      {showMemberConnector && (
        <>
          <span
            aria-hidden="true"
            className="absolute left-0 top-1/2 h-px w-4 -translate-x-full -translate-y-1/2 bg-sky-500/35"
          />
          <span
            aria-hidden="true"
            className={cn(
              "absolute -left-4 w-px bg-sky-500/35",
              connectorLineClass,
            )}
          />
        </>
      )}
      <div
        className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary",
          oauthPool?.role === "owner" && "bg-primary/12 text-primary",
          oauthPool?.role === "member" && "bg-sky-500/12 text-sky-700 dark:text-sky-300",
        )}
      >
        <Cpu className="h-4 w-4" />
      </div>

      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-semibold">{displayName}</span>
          <span
            className={cn(
              "inline-block h-2 w-2 shrink-0 rounded-full",
              provider.enabled ? "bg-emerald-500" : "bg-muted-foreground/40",
            )}
          />
          {hasPoolRole && (
            <Badge
              variant={oauthPool.role === "owner" ? "outline" : "info"}
              className={cn(
                "h-5 px-1.5 text-[10px]",
                oauthPool.role === "owner" && "border-primary/30 bg-primary/[0.06] text-primary",
              )}
            >
              {t(oauthPool.role === "owner" ? "list.poolOwner" : "list.poolMember")}
            </Badge>
          )}
        </div>
        {(secondaryText || availabilityWarningLabel || showQuota) && (
          <div className="flex min-w-0 items-center gap-2 text-xs">
            {secondaryText ? (
              <span className="min-w-0 flex-1 truncate text-muted-foreground">
                {secondaryText}
              </span>
            ) : (
              <span className="flex-1" />
            )}
            {availabilityWarningLabel && (
              <span
                className={cn(
                  "shrink-0 font-medium",
                  oauthPool?.availability === "disabled"
                    ? "text-muted-foreground"
                    : "text-amber-700 dark:text-amber-400",
                )}
              >
                {availabilityWarningLabel}
              </span>
            )}
            {showQuota && (
              <ChatGPTOAuthQuotaStrip
                quota={oauthPool?.quota}
                loading={oauthPool?.quotaLoading}
                compact
                layout="inline"
                embedded
                translationNamespace="providers"
                translationKeyPrefix="quota"
                className="shrink-0"
              />
            )}
          </div>
        )}
      </div>

      <div className="hidden shrink-0 sm:block">
        <Badge variant={typeBadge.variant} className="text-[11px]">
          {typeBadge.label}
        </Badge>
      </div>

      <div className="hidden shrink-0 md:block">
        <ProviderApiKeyBadge provider={provider} oauthAvailability={oauthPool?.availability} />
      </div>

      <div className="hidden shrink-0 text-[11px] text-muted-foreground lg:block">
        {provider.enabled ? tc("enabled") : tc("disabled")}
      </div>

      {onDelete && (
        <Button
          variant="ghost"
          size="xs"
          className="shrink-0 text-muted-foreground hover:text-destructive"
          onClick={(event) => {
            event.stopPropagation();
            onDelete();
          }}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      )}
    </div>
  );
}
