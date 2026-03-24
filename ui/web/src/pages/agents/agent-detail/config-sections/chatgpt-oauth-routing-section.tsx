import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";
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
  canManageProviders?: boolean;
  className?: string;
}

function statusBadgeVariant(availability: ChatGPTOAuthAvailability): "success" | "warning" | "outline" {
  if (availability === "ready") return "success";
  if (availability === "needs_sign_in") return "warning";
  return "outline";
}

export function ChatGPTOAuthRoutingSection({
  currentProvider,
  providers,
  value,
  onChange,
  canManageProviders = true,
  className,
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
      role: "preferred" as const,
    },
    ...allExtraProviders
      .filter((provider) => selectedExtras.has(provider.name))
      .map((provider) => ({
        name: provider.name,
        label: provider.display_name || provider.name,
        availability: getAvailability(provider),
        role: "extra" as const,
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
    <Card className={cn("gap-3 overflow-hidden", className)}>
      <CardHeader className="border-b bg-muted/20">
        <CardTitle>{t("chatgptOAuthRouting.title")}</CardTitle>
        <CardDescription>{t("chatgptOAuthRouting.description")}</CardDescription>
        {!canManageProviders && (
          <p className="text-xs text-muted-foreground">
            {t("chatgptOAuthRouting.providerAccessInline")}
          </p>
        )}
      </CardHeader>

      <CardContent className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto">
        <section className="space-y-3">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {t("chatgptOAuthRouting.defaultAccount")}
          </p>
          <div className="rounded-lg border bg-muted/20 p-3.5">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium">
                    {currentOAuthProvider.display_name || currentOAuthProvider.name}
                  </span>
                  <Badge variant="secondary">{t("chatgptOAuthRouting.role.preferred")}</Badge>
                  <Badge variant={statusBadgeVariant(preferredAvailability)}>
                    {t(`chatgptOAuthRouting.status.${preferredAvailability}`)}
                  </Badge>
                </div>
                <p className="font-mono text-xs text-muted-foreground">{currentOAuthProvider.name}</p>
              </div>
              <p className="max-w-xs text-xs text-muted-foreground">
                {t("chatgptOAuthRouting.defaultHint")}
              </p>
            </div>
          </div>
        </section>

        <section className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {t("chatgptOAuthRouting.strategyLabel")}
            </p>
            <Badge variant="outline">
              {value.strategy === "round_robin"
                ? t("chatgptOAuthRouting.strategy.roundRobin")
                : t("chatgptOAuthRouting.strategy.manual")}
            </Badge>
          </div>

          <div className="grid gap-2 sm:grid-cols-2">
            <Button
              type="button"
              variant={value.strategy === "manual" ? "default" : "outline"}
              onClick={() => setStrategy("manual")}
            >
              {t("chatgptOAuthRouting.strategy.manual")}
            </Button>
            <Button
              type="button"
              variant={value.strategy === "round_robin" ? "default" : "outline"}
              onClick={() => setStrategy("round_robin")}
            >
              {t("chatgptOAuthRouting.strategy.roundRobin")}
            </Button>
          </div>

          <p className="text-xs text-muted-foreground">
            {t("chatgptOAuthRouting.strategyHint")}
          </p>
        </section>

        <section className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {t("chatgptOAuthRouting.extraAccountsLabel")}
            </p>
            {readyExtraProviders.length > 0 && (
              <Badge variant="outline">
                {t("chatgptOAuthRouting.readySummary", {
                  ready: readyExtraProviders.length,
                  total: allExtraProviders.length,
                })}
              </Badge>
            )}
          </div>

          {isLoading ? (
            <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.loadingAccounts")}
            </div>
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
            <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.noReadyExtras")}
            </div>
          )}
        </section>

        <div className="space-y-4">
          <section className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                {t("chatgptOAuthRouting.selectedAccountsLabel")}
              </p>
              <Badge variant="outline">
                {t("chatgptOAuthRouting.selectedCount", { count: selectedEntries.length })}
              </Badge>
            </div>

            <div className="rounded-lg border bg-muted/20 p-3.5">
              <div className="flex flex-wrap gap-2">
                {selectedEntries.map((entry) => (
                  <div key={entry.name} className="flex flex-wrap items-center gap-1.5 rounded-md border bg-background px-2 py-1">
                    <span className="text-xs font-medium">{entry.label}</span>
                    <Badge variant={entry.role === "preferred" ? "secondary" : "outline"}>
                      {t(`chatgptOAuthRouting.role.${entry.role}`)}
                    </Badge>
                    <Badge variant={statusBadgeVariant(entry.availability)}>
                      {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                    </Badge>
                  </div>
                ))}
              </div>
            </div>
          </section>

          {attentionEntries.length > 0 && (
            <section className="space-y-3">
              <div className="flex items-center gap-2">
                <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400" />
                <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                  {t("chatgptOAuthRouting.attentionTitle")}
                </p>
              </div>

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
                    <Badge key={entry.name} variant={statusBadgeVariant(entry.availability)}>
                      {entry.label} · {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                    </Badge>
                  )
                ))}
              </div>

              <p className="text-xs text-muted-foreground">
                {t("chatgptOAuthRouting.attentionHint")}
              </p>
            </section>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
