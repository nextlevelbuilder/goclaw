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
import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
import type { ProviderData } from "@/types/provider";

interface ChatGPTOAuthRoutingSectionProps {
  currentProvider: string;
  providers: ProviderData[];
  value: ChatGPTOAuthRoutingConfig;
  onChange: (value: ChatGPTOAuthRoutingConfig) => void;
}

export function ChatGPTOAuthRoutingSection({
  currentProvider,
  providers,
  value,
  onChange,
}: ChatGPTOAuthRoutingSectionProps) {
  const { t } = useTranslation("agents");

  const oauthProviders = providers.filter((provider) => provider.provider_type === "chatgpt_oauth");
  const currentOAuthProvider = oauthProviders.find((provider) => provider.name === currentProvider);
  if (!currentOAuthProvider) {
    return null;
  }

  const allExtraProviders = oauthProviders.filter((provider) => provider.name !== currentProvider);
  const extraProviders = allExtraProviders.filter((provider) => provider.enabled);
  const selectedExtras = new Set(value.extra_provider_names ?? []);
  const selectedBadges = [
    currentOAuthProvider.display_name || currentOAuthProvider.name,
    ...allExtraProviders
      .filter((provider) => selectedExtras.has(provider.name))
      .map((provider) => provider.display_name || provider.name),
  ];

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
            <span className="text-xs text-muted-foreground">
              {t("chatgptOAuthRouting.defaultHint")}
            </span>
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
          {extraProviders.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {extraProviders.map((provider) => {
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
            <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.emptyExtras")}</p>
          )}
          <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.extraHint")}</p>
        </div>

        <div className="space-y-2">
          <Label>{t("chatgptOAuthRouting.selectedAccountsLabel")}</Label>
          <div className="flex flex-wrap gap-2">
            {selectedBadges.map((name) => (
              <Badge key={name} variant="outline">{name}</Badge>
            ))}
          </div>
        </div>
      </div>
    </>
  );
}
