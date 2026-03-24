import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import {
  useChatGPTOAuthProviderStatuses,
  type ChatGPTOAuthAvailability,
} from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
import type { ProviderData } from "@/types/provider";

interface ChatGPTOAuthRoutingSectionProps {
  currentProvider: string;
  providers: ProviderData[];
  value: ChatGPTOAuthRoutingConfig;
  onChange: (value: ChatGPTOAuthRoutingConfig) => void;
}

function statusBadgeClass(availability: ChatGPTOAuthAvailability): string {
  if (availability === "ready") return "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
  if (availability === "disabled") return "border-muted-foreground/30 bg-muted text-muted-foreground";
  return "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300";
}

export function ChatGPTOAuthRoutingSection({
  currentProvider,
  providers,
  value,
  onChange,
}: ChatGPTOAuthRoutingSectionProps) {
  const { t } = useTranslation("agents");
  const { statuses, isLoading } = useChatGPTOAuthProviderStatuses(providers);

  const oauthProviders = providers.filter((provider) => provider.provider_type === "chatgpt_oauth");
  const currentOAuthProvider = oauthProviders.find((provider) => provider.name === currentProvider);
  if (!currentOAuthProvider) return null;

  const statusByName = useMemo(
    () => new Map(statuses.map((status) => [status.provider.name, status])),
    [statuses],
  );
  const getAvailability = (provider: ProviderData): ChatGPTOAuthAvailability => (
    statusByName.get(provider.name)?.availability
      ?? (provider.enabled ? "needs_sign_in" : "disabled")
  );

  const allExtraProviders = oauthProviders.filter((provider) => provider.name !== currentProvider);
  const readyExtraProviders = allExtraProviders.filter((provider) => getAvailability(provider) === "ready");
  const selectedExtras = new Set(value.extra_provider_names ?? []);
  const preferredAvailability = getAvailability(currentOAuthProvider);
  const selectedEntries = [
    {
      name: currentOAuthProvider.name,
      label: currentOAuthProvider.display_name || currentOAuthProvider.name,
      availability: preferredAvailability,
    },
    ...allExtraProviders
      .filter((provider) => selectedExtras.has(provider.name))
      .map((provider) => ({
        name: provider.name,
        label: provider.display_name || provider.name,
        availability: getAvailability(provider),
      })),
  ];
  const attentionEntries = allExtraProviders
    .filter((provider) => getAvailability(provider) !== "ready")
    .map((provider) => ({
      name: provider.name,
      label: provider.display_name || provider.name,
      availability: getAvailability(provider),
      selected: selectedExtras.has(provider.name),
    }));

  const setStrategy = (strategy: "manual" | "round_robin") => {
    onChange({ ...value, strategy });
  };

  const toggleProvider = (providerName: string) => {
    const next = new Set(selectedExtras);
    if (next.has(providerName)) {
      next.delete(providerName);
    } else {
      next.add(providerName);
    }
    onChange({
      ...value,
      extra_provider_names: Array.from(next),
    });
  };

  return (
    <>
      <ConfigGroupHeader
        title={t("chatgptOAuthRouting.title")}
        description={t("chatgptOAuthRouting.description")}
      />

      <div className="space-y-4 rounded-lg border p-3 sm:p-4">
        <div className="space-y-2">
          <Label>{t("chatgptOAuthRouting.defaultAccount")}</Label>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">{currentOAuthProvider.display_name || currentOAuthProvider.name}</Badge>
            <Badge variant="outline" className={statusBadgeClass(preferredAvailability)}>
              {t(`chatgptOAuthRouting.status.${preferredAvailability}`)}
            </Badge>
            <span className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.defaultHint")}</span>
          </div>
        </div>

        <div className="space-y-2">
          <Label>{t("chatgptOAuthRouting.strategyLabel")}</Label>
          <Select
            value={value.strategy || "manual"}
            onValueChange={(next) => setStrategy(next as "manual" | "round_robin")}
          >
            <SelectTrigger className="text-base md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="manual">{t("chatgptOAuthRouting.strategy.manual")}</SelectItem>
              <SelectItem value="round_robin">{t("chatgptOAuthRouting.strategy.roundRobin")}</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.strategyHint")}</p>
        </div>

        <div className="space-y-2">
          <Label>{t("chatgptOAuthRouting.extraAccountsLabel")}</Label>
          {isLoading ? (
            <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.loadingAccounts")}</p>
          ) : readyExtraProviders.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {readyExtraProviders.map((provider) => {
                const selected = selectedExtras.has(provider.name);
                return (
                  <Button
                    key={provider.name}
                    type="button"
                    variant={selected ? "default" : "outline"}
                    size="sm"
                    onClick={() => toggleProvider(provider.name)}
                  >
                    {provider.display_name || provider.name}
                  </Button>
                );
              })}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.noReadyExtras")}</p>
          )}
          <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.extraSelectableHint")}</p>
        </div>

        <div className="space-y-2">
          <Label>{t("chatgptOAuthRouting.selectedAccountsLabel")}</Label>
          <div className="flex flex-wrap gap-2">
            {selectedEntries.map((entry) => (
              <div key={entry.name} className="flex flex-wrap items-center gap-1.5 rounded-md border px-2 py-1">
                <span className="text-xs font-medium">{entry.label}</span>
                <Badge variant="outline" className={statusBadgeClass(entry.availability)}>
                  {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                </Badge>
              </div>
            ))}
          </div>
        </div>

        {attentionEntries.length > 0 && (
          <div className="space-y-2">
            <Label>{t("chatgptOAuthRouting.attentionTitle")}</Label>
            <div className="flex flex-wrap gap-2">
              {attentionEntries.map((entry) => (
                entry.selected ? (
                  <Button
                    key={entry.name}
                    type="button"
                    variant="outline"
                    size="sm"
                    className="border-amber-500/30 text-amber-700 dark:text-amber-300"
                    onClick={() => toggleProvider(entry.name)}
                  >
                    {entry.label} · {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                  </Button>
                ) : (
                  <Badge key={entry.name} variant="outline" className={statusBadgeClass(entry.availability)}>
                    {entry.label} · {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                  </Badge>
                )
              ))}
            </div>
            <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.attentionHint")}</p>
          </div>
        )}
      </div>
    </>
  );
}
