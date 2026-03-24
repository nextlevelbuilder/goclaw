import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Settings2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import {
  useChatGPTOAuthProviderStatuses,
  type ChatGPTOAuthAvailability,
} from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import type { AgentData } from "@/types/agent";
import { normalizeChatGPTOAuthRouting } from "../agent-display-utils";

interface ChatGPTOAuthRoutingSummarySectionProps {
  agent: AgentData;
  onManage: () => void;
}

function statusBadgeClass(availability: ChatGPTOAuthAvailability): string {
  if (availability === "ready") return "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
  if (availability === "disabled") return "border-muted-foreground/30 bg-muted text-muted-foreground";
  return "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300";
}

export function ChatGPTOAuthRoutingSummarySection({
  agent,
  onManage,
}: ChatGPTOAuthRoutingSummarySectionProps) {
  const { t } = useTranslation("agents");
  const { providers } = useProviders();
  const { statuses, isLoading } = useChatGPTOAuthProviderStatuses(providers);
  const routing = normalizeChatGPTOAuthRouting(agent.other_config);
  const providerByName = useMemo(
    () => new Map(providers.map((provider) => [provider.name, provider])),
    [providers],
  );
  const statusByName = useMemo(
    () => new Map(statuses.map((status) => [status.provider.name, status])),
    [statuses],
  );
  const currentProvider = providerByName.get(agent.provider);
  const shouldShow = currentProvider?.provider_type === "chatgpt_oauth"
    || routing.extraProviderNames.length > 0
    || routing.strategy === "round_robin";

  if (!shouldShow) return null;

  const preferredLabel = currentProvider?.display_name || agent.provider;
  const preferredAvailability = statusByName.get(agent.provider)?.availability
    ?? (currentProvider?.enabled === false ? "disabled" : "needs_sign_in");
  const extraEntries = routing.extraProviderNames.map((providerName) => {
    const provider = providerByName.get(providerName);
    const availability = statusByName.get(providerName)?.availability
      ?? (provider?.enabled === false ? "disabled" : "needs_sign_in");
    return {
      providerName,
      label: provider?.display_name || providerName,
      availability,
    };
  });
  const readyExtraCount = extraEntries.filter((entry) => entry.availability === "ready").length;

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-0.5">
          <h3 className="text-sm font-medium">{t("chatgptOAuthRouting.summaryTitle")}</h3>
          <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.summaryDescription")}</p>
        </div>
        <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={onManage}>
          <Settings2 className="h-4 w-4" />
          {t("chatgptOAuthRouting.manageAction")}
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="space-y-2">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {t("chatgptOAuthRouting.defaultAccount")}
          </p>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">{preferredLabel}</Badge>
            <Badge variant="outline" className={statusBadgeClass(preferredAvailability)}>
              {t(`chatgptOAuthRouting.status.${preferredAvailability}`)}
            </Badge>
          </div>
        </div>

        <div className="space-y-2">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {t("chatgptOAuthRouting.strategyLabel")}
          </p>
          <Badge variant="outline">
            {routing.strategy === "round_robin"
              ? t("chatgptOAuthRouting.strategy.roundRobin")
              : t("chatgptOAuthRouting.strategy.manual")}
          </Badge>
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
              : extraEntries.length === 0
                ? t("chatgptOAuthRouting.singleAccountHint")
                : t("chatgptOAuthRouting.readySummary", { ready: readyExtraCount, total: extraEntries.length })}
          </p>
          {isLoading && <p>{t("chatgptOAuthRouting.loadingAccounts")}</p>}
        </AlertDescription>
      </Alert>
    </section>
  );
}
