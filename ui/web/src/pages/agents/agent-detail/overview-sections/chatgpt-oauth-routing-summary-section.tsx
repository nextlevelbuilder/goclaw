import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Settings2 } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import {
  useChatGPTOAuthProviderStatuses,
  type ChatGPTOAuthAvailability,
} from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import { useChatGPTOAuthProviderQuotas } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import { useAuthStore } from "@/stores/use-auth-store";
import type { AgentData } from "@/types/agent";
import { getChatGPTOAuthProviderRouting } from "@/types/provider";
import { ChatGPTOAuthQuotaBadges } from "../chatgpt-oauth-quota-badges";
import {
  normalizeChatGPTOAuthRouting,
  resolveEffectiveChatGPTOAuthRouting,
  strategyLabelKey,
} from "../agent-display-utils";
import { summarizeQuotaHealth } from "../chatgpt-oauth-quota-utils";

interface ChatGPTOAuthRoutingSummarySectionProps {
  agent: AgentData;
  onManage: () => void;
}

function statusBadgeClass(availability: ChatGPTOAuthAvailability): string {
  if (availability === "ready") {
    return "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
  }
  if (availability === "disabled") {
    return "border-muted-foreground/30 bg-muted text-muted-foreground";
  }
  return "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300";
}


export function ChatGPTOAuthRoutingSummarySection({
  agent,
  onManage,
}: ChatGPTOAuthRoutingSummarySectionProps) {
  const { t } = useTranslation("agents");
  const role = useAuthStore((state) => state.role);
  const canManagePool = role === "admin" || role === "owner";
  const { providers } = useProviders(canManagePool);
  const { statuses, isLoading } = useChatGPTOAuthProviderStatuses(providers, canManagePool);
  const providerByName = useMemo(
    () => new Map(providers.map((provider) => [provider.name, provider])),
    [providers],
  );
  const statusByName = useMemo(
    () => new Map(statuses.map((status) => [status.provider.name, status])),
    [statuses],
  );
  const currentProvider = providerByName.get(agent.provider);
  const savedRouting = normalizeChatGPTOAuthRouting(agent.other_config);
  const providerDefaults = getChatGPTOAuthProviderRouting(currentProvider?.settings);
  const effectiveRouting = resolveEffectiveChatGPTOAuthRouting(
    agent.provider,
    currentProvider?.settings,
    savedRouting,
  );
  const shouldShow = currentProvider?.provider_type === "chatgpt_oauth";
  const providerNames = effectiveRouting.poolProviderNames.filter(
    (providerName) => providerByName.get(providerName)?.provider_type === "chatgpt_oauth",
  );
  const { quotaByName } = useChatGPTOAuthProviderQuotas(providerNames, shouldShow);

  if (!canManagePool) return null;
  if (!shouldShow) return null;

  const preferredLabel = currentProvider?.display_name || agent.provider;
  const preferredAvailability = statusByName.get(agent.provider)?.availability
    ?? (currentProvider?.enabled === false ? "disabled" : "needs_sign_in");
  const extraEntries = effectiveRouting.extraProviderNames.map((providerName) => {
    const provider = providerByName.get(providerName);
    const availability = statusByName.get(providerName)?.availability
      ?? (provider?.enabled === false ? "disabled" : "needs_sign_in");
    return {
      providerName,
      label: provider?.display_name || providerName,
      availability,
      quota: quotaByName.get(providerName),
    };
  });
  const readyExtraCount = extraEntries.filter((entry) => entry.availability === "ready").length;
  const quotaEntries = [
    { availability: preferredAvailability, quota: quotaByName.get(agent.provider) },
    ...extraEntries.map((entry) => ({ availability: entry.availability, quota: entry.quota })),
  ];
  const quotaSummary = summarizeQuotaHealth(
    quotaEntries.filter((entry) => entry.availability === "ready"),
  );
  const quotaSummaryTotal = quotaEntries.filter(
    (entry) => entry.availability === "ready",
  ).length;

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="space-y-2.5">
        <div className="flex flex-wrap items-start gap-3">
          <div className="min-w-0 flex-1 space-y-0.5">
            <h3 className="text-sm font-medium">{t("chatgptOAuthRouting.summaryTitle")}</h3>
            <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.summaryDescription")}</p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="ml-auto shrink-0 gap-1.5 self-start"
            onClick={onManage}
          >
            <Settings2 className="h-4 w-4" />
            {t("chatgptOAuthRouting.manageAction")}
          </Button>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="outline">{t(strategyLabelKey(effectiveRouting.strategy))}</Badge>
          <Badge variant={effectiveRouting.overrideMode === "inherit" ? "secondary" : "outline"}>
            {effectiveRouting.overrideMode === "inherit"
              ? t("chatgptOAuthRouting.mode.inherit")
              : t("chatgptOAuthRouting.mode.custom")}
          </Badge>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <div className="space-y-2 rounded-lg border bg-muted/10 p-3">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {t("chatgptOAuthRouting.defaultAccount")}
          </p>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">{preferredLabel}</Badge>
            <Badge variant="outline" className={statusBadgeClass(preferredAvailability)}>
              {t(`chatgptOAuthRouting.status.${preferredAvailability}`)}
            </Badge>
            <ChatGPTOAuthQuotaBadges quota={quotaByName.get(agent.provider)} />
          </div>
        </div>

        <div className="space-y-2 rounded-lg border bg-muted/10 p-3">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {t("chatgptOAuthRouting.selectedAccountsLabel")}
          </p>
          <p className="text-sm font-semibold">
            {t("chatgptOAuthRouting.selectedCount", {
              count: effectiveRouting.poolProviderNames.length,
            })}
          </p>
          <p className="text-xs text-muted-foreground">
            {effectiveRouting.overrideMode === "inherit" && providerDefaults
              ? t("chatgptOAuthRouting.mode.summaryInherited")
              : t("chatgptOAuthRouting.mode.summaryCustom")}
          </p>
        </div>

        <div className="space-y-2 rounded-lg border bg-muted/10 p-3">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {t("chatgptOAuthRouting.metrics.quotaReadyAccounts")}
          </p>
          <p className="text-sm font-semibold">
            {t("chatgptOAuthRouting.readySummary", {
              ready: readyExtraCount,
              total: extraEntries.length,
            })}
          </p>
          <p className="text-xs text-muted-foreground">
            {quotaSummary.attention > 0
              ? t("chatgptOAuthRouting.quota.needsAttention", { count: quotaSummary.attention })
              : t("chatgptOAuthRouting.quota.healthySummary", {
                  usable: quotaSummary.usable,
                  total: quotaSummaryTotal,
                })}
          </p>
        </div>
      </div>

      <div className="space-y-2">
        <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {t("chatgptOAuthRouting.extraAccountsLabel")}
        </p>
        {extraEntries.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {extraEntries.map((entry) => (
              <div key={entry.providerName} className="flex flex-wrap items-center gap-1.5 rounded-md border px-2 py-1">
                <span className="text-xs font-medium">{entry.label}</span>
                <Badge variant="outline" className={statusBadgeClass(entry.availability)}>
                  {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                </Badge>
                <ChatGPTOAuthQuotaBadges quota={entry.quota} />
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.emptySelected")}</p>
        )}
      </div>

      <Alert>
        <AlertDescription>
          <p>
            {preferredAvailability !== "ready"
              ? t("chatgptOAuthRouting.preferredNeedsAttention")
              : quotaSummary.attention > 0
                ? t("chatgptOAuthRouting.quota.needsAttention", { count: quotaSummary.attention })
                : extraEntries.length === 0
                  ? t("chatgptOAuthRouting.singleAccountHint")
                  : t("chatgptOAuthRouting.readySummary", { ready: readyExtraCount, total: extraEntries.length })}
          </p>
          {isLoading ? <p>{t("chatgptOAuthRouting.loadingAccounts")}</p> : null}
        </AlertDescription>
      </Alert>
    </section>
  );
}
