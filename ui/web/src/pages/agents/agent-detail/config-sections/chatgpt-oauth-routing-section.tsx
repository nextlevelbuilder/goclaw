import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { cn } from "@/lib/utils";
import {
  useChatGPTOAuthProviderStatuses,
  type ChatGPTOAuthAvailability,
} from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import type { ChatGPTOAuthProviderQuota } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import type {
  ChatGPTOAuthRoutingConfig,
  EffectiveChatGPTOAuthRoutingStrategy,
} from "@/types/agent";
import type { ProviderData } from "@/types/provider";
import type { CodexPoolEntry } from "../codex-pool-entry-types";
import { getQuotaFailureKind, getRouteReadiness } from "../chatgpt-oauth-quota-utils";
import {
  MembershipSection,
  SelectedAccountsSection,
  PoolStateSection,
} from "./chatgpt-oauth-pool-sections";

interface ChatGPTOAuthRoutingSectionProps {
  title?: string;
  description?: string;
  currentProvider: string;
  providers: ProviderData[];
  value: ChatGPTOAuthRoutingConfig;
  onChange: (value: ChatGPTOAuthRoutingConfig) => void;
  showOverrideMode?: boolean;
  defaultRouting?: {
    strategy: EffectiveChatGPTOAuthRoutingStrategy;
    extraProviderNames: string[];
  } | null;
  canManageProviders?: boolean;
  membershipEditable?: boolean;
  membershipManagedByLabel?: string;
  quotaByName?: Map<string, ChatGPTOAuthProviderQuota>;
  quotaLoading?: boolean;
  entries?: CodexPoolEntry[];
  isDirty?: boolean;
  saving?: boolean;
  onSave?: () => void;
  contentScrollable?: boolean;
  className?: string;
}

function getAvailabilityFromMap(
  provider: ProviderData,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
): ChatGPTOAuthAvailability {
  return (
    statusByName.get(provider.name)?.availability ??
    (provider.enabled ? "needs_sign_in" : "disabled")
  );
}

export function ChatGPTOAuthRoutingSection({
  title,
  description,
  currentProvider,
  providers,
  value,
  onChange,
  showOverrideMode = true,
  defaultRouting = null,
  canManageProviders = true,
  membershipEditable = true,
  membershipManagedByLabel,
  quotaByName,
  quotaLoading = false,
  entries = [],
  isDirty = false,
  saving = false,
  onSave,
  contentScrollable = false,
  className,
}: ChatGPTOAuthRoutingSectionProps) {
  const { t } = useTranslation("agents");
  const { t: tc } = useTranslation("common");
  const { statuses, isLoading } = useChatGPTOAuthProviderStatuses(providers);

  const statusByName = useMemo(
    () => new Map(statuses.map((s) => [s.provider.name, s])),
    [statuses],
  );

  const oauthProviders = providers.filter(
    (p) => p.provider_type === "chatgpt_oauth",
  );
  const currentOAuthProvider = oauthProviders.find(
    (p) => p.name === currentProvider,
  );
  if (!currentOAuthProvider) return null;

  const allExtraProviders = oauthProviders.filter((p) => p.name !== currentProvider);
  const selectedExtras = new Set(value.extra_provider_names ?? []);
  const selectableExtraProviders = allExtraProviders.filter(
    (p) =>
      selectedExtras.has(p.name) ||
      getAvailabilityFromMap(p, statusByName) === "ready",
  );
  const mode = value.override_mode === "inherit" ? "inherit" : "custom";
  const providerDefaultsAvailable =
    defaultRouting != null && defaultRouting.extraProviderNames.length > 0;

  const selectedEntries = entries.map((entry) => ({
    ...entry,
    routeReadiness: getRouteReadiness(entry.availability, entry.quota),
    failureKind: getQuotaFailureKind(entry.quota),
  }));
  const healthyEntries = selectedEntries.filter((e) => e.routeReadiness === "healthy");
  const standbyEntries = selectedEntries.filter(
    (e) => e.routeReadiness === "fallback" || e.routeReadiness === "checking",
  );
  const blockedEntries = selectedEntries.filter((e) => e.routeReadiness === "blocked");
  const routerActiveEntries = healthyEntries;

  const selectedStrategy: EffectiveChatGPTOAuthRoutingStrategy =
    value.strategy === "round_robin" || value.strategy === "priority_order"
      ? value.strategy
      : "primary_first";
  const canEditMembership = canManageProviders && membershipEditable;
  const canUsePoolStrategies =
    canManageProviders &&
    mode !== "inherit" &&
    (membershipEditable || providerDefaultsAvailable || selectedEntries.length > 1);

  const setMode = (overrideMode: "inherit" | "custom") =>
    onChange({ ...value, override_mode: overrideMode });

  const setStrategy = (strategy: EffectiveChatGPTOAuthRoutingStrategy) =>
    onChange({ ...value, strategy });

  const toggleProvider = (providerName: string) => {
    const next = new Set(selectedExtras);
    if (next.has(providerName)) next.delete(providerName);
    else next.add(providerName);
    onChange({ ...value, extra_provider_names: Array.from(next) });
  };

  const routeDetail = (entry: (typeof selectedEntries)[number]): string | undefined => {
    if (entry.availability !== "ready")
      return t(`chatgptOAuthRouting.status.${entry.availability}`);
    if (entry.failureKind)
      return t(`chatgptOAuthRouting.quota.failure.${entry.failureKind}.label`);
    if (entry.routeReadiness === "checking")
      return t("chatgptOAuthRouting.quota.checking");
    return undefined;
  };

  return (
    <Card className={cn("flex min-h-0 flex-col gap-0 overflow-hidden", className)}>
      <CardHeader className="border-b bg-muted/20 px-3 py-2 lg:px-4 lg:py-2.5 [@media(max-height:860px)]:py-1.5">
        <div className="flex items-start justify-between gap-1.5">
          <div className="min-w-0">
            <CardTitle className="text-sm sm:text-[15px] lg:text-base [@media(max-height:860px)]:text-[14px]">
              {title ?? t("chatgptOAuthRouting.controlTitle")}
            </CardTitle>
            <CardDescription className="mt-0.5 hidden text-xs text-muted-foreground 2xl:block 2xl:line-clamp-2 [@media(min-width:1800px)]:line-clamp-none [@media(max-height:860px)]:hidden">
              {description ?? t("chatgptOAuthRouting.controlDescription")}
            </CardDescription>
          </div>
          <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
            {showOverrideMode ? (
              <Badge
                variant={mode === "inherit" ? "secondary" : "outline"}
                className="h-6 px-2 text-[11px] [@media(max-height:860px)]:h-5"
              >
                {mode === "inherit"
                  ? t("chatgptOAuthRouting.mode.inherit")
                  : t("chatgptOAuthRouting.mode.custom")}
              </Badge>
            ) : null}
            {!canManageProviders ? (
              <Badge variant="outline" className="h-6 px-2 text-[11px] [@media(max-height:860px)]:h-5">
                {t("chatgptOAuthRouting.viewerMode")}
              </Badge>
            ) : null}
            {isDirty ? (
              <Badge variant="warning" className="h-6 px-2 text-[11px] [@media(max-height:860px)]:h-5">
                {t("chatgptOAuthRouting.draftBadge")}
              </Badge>
            ) : null}
            {(quotaLoading || isLoading) ? (
              <Badge variant="outline" className="h-6 px-2 text-[11px] [@media(max-height:860px)]:h-5">
                {t("chatgptOAuthRouting.quota.checking")}
              </Badge>
            ) : null}
          </div>
        </div>
      </CardHeader>

      <CardContent
        className={cn(
          "min-h-0 flex-1 space-y-3 px-3 py-2.5 lg:px-4 lg:py-3 [@media(max-height:760px)]:space-y-2 [@media(max-height:760px)]:py-2",
          contentScrollable ? "overflow-y-auto" : "overflow-visible",
        )}
      >
        {showOverrideMode ? (
          <section className="space-y-2.5 [@media(max-height:760px)]:space-y-2">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {t("chatgptOAuthRouting.mode.label")}
            </p>
            <div className="grid gap-1.5 xl:grid-cols-2">
              <Button
                type="button"
                variant={mode === "inherit" ? "default" : "outline"}
                onClick={() => setMode("inherit")}
                disabled={!canManageProviders}
                className="h-9 [@media(max-height:760px)]:h-8"
              >
                {t("chatgptOAuthRouting.mode.inherit")}
              </Button>
              <Button
                type="button"
                variant={mode === "custom" ? "default" : "outline"}
                onClick={() => setMode("custom")}
                disabled={!canManageProviders}
                className="h-9 [@media(max-height:760px)]:h-8"
              >
                {t("chatgptOAuthRouting.mode.custom")}
              </Button>
            </div>
            {!providerDefaultsAvailable ? (
              <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
                {t("chatgptOAuthRouting.mode.noProviderDefault")}
              </div>
            ) : null}
          </section>
        ) : null}

        <section className="space-y-2.5 [@media(max-height:760px)]:space-y-2">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {t("chatgptOAuthRouting.strategyLabel")}
          </p>
          <div className="grid gap-2 sm:grid-cols-3">
            <Button
              type="button"
              variant={selectedStrategy === "primary_first" ? "default" : "outline"}
              onClick={() => setStrategy("primary_first")}
              disabled={!canManageProviders || mode === "inherit"}
              className="h-9 text-xs sm:text-sm [@media(max-height:760px)]:h-8"
            >
              {t("chatgptOAuthRouting.strategy.primaryFirst")}
            </Button>
            <Button
              type="button"
              variant={selectedStrategy === "round_robin" ? "default" : "outline"}
              onClick={() => setStrategy("round_robin")}
              disabled={!canUsePoolStrategies}
              className="h-9 text-xs sm:text-sm [@media(max-height:760px)]:h-8"
            >
              {t("chatgptOAuthRouting.strategy.roundRobin")}
            </Button>
            <Button
              type="button"
              variant={selectedStrategy === "priority_order" ? "default" : "outline"}
              onClick={() => setStrategy("priority_order")}
              disabled={!canUsePoolStrategies}
              className="h-9 text-xs sm:text-sm [@media(max-height:760px)]:h-8"
            >
              {t("chatgptOAuthRouting.strategy.priorityOrder")}
            </Button>
          </div>
        </section>

        <MembershipSection
          membershipEditable={membershipEditable}
          membershipManagedByLabel={membershipManagedByLabel}
          currentProvider={currentProvider}
          selectedEntries={selectedEntries}
          selectableExtraProviders={selectableExtraProviders}
          selectedExtras={selectedExtras}
          quotaByName={quotaByName}
          canEditMembership={canEditMembership}
          mode={mode}
          isLoading={isLoading}
          onToggleProvider={toggleProvider}
        />

        <SelectedAccountsSection selectedEntries={selectedEntries} />

        <PoolStateSection
          routerActiveEntries={routerActiveEntries}
          standbyEntries={standbyEntries}
          blockedEntries={blockedEntries}
          routeDetail={routeDetail}
        />
      </CardContent>

      {canManageProviders && onSave && (isDirty || saving) ? (
        <div className="border-t bg-background/70 px-3 py-2 lg:px-4 [@media(max-height:760px)]:py-1.5">
          <div className="flex items-center justify-end">
            <Button
              type="button"
              size="sm"
              onClick={onSave}
              disabled={!isDirty || saving}
            >
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {saving ? tc("saving") : tc("save")}
            </Button>
          </div>
        </div>
      ) : null}
    </Card>
  );
}
