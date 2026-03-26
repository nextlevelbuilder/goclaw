import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Check, Loader2 } from "lucide-react";
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
import type { CodexPoolEntry } from "../codex-pool-activity-panel";
import {
  getQuotaFailureKind,
  getRouteReadiness,
} from "../chatgpt-oauth-quota-utils";

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

function statusBadgeVariant(
  availability: ChatGPTOAuthAvailability,
): "success" | "warning" | "outline" {
  if (availability === "ready") return "success";
  if (availability === "needs_sign_in") return "warning";
  return "outline";
}

function routeBadgeVariant(
  readiness: ReturnType<typeof getRouteReadiness>,
): "success" | "warning" | "outline" | "destructive" {
  if (readiness === "healthy") return "success";
  if (readiness === "fallback") return "warning";
  if (readiness === "checking") return "outline";
  return "destructive";
}

function routeLabelKey(
  readiness: ReturnType<typeof getRouteReadiness>,
): string {
  if (readiness === "healthy") return "chatgptOAuthRouting.routerActiveTitle";
  if (readiness === "fallback") return "chatgptOAuthRouting.fallbackTitle";
  if (readiness === "checking") return "chatgptOAuthRouting.checkingTitle";
  return "chatgptOAuthRouting.blockedNowTitle";
}

function roleBadgeClass(role: "preferred" | "extra"): string {
  if (role === "preferred") {
    return "border-primary/35 bg-primary/12 text-foreground shadow-sm dark:border-primary/40 dark:bg-primary/18";
  }
  return "border-border/70 bg-background/80 text-muted-foreground";
}

function StateGroup({
  title,
  count,
  variant,
  entries,
  emptyLabel,
}: {
  title: string;
  count: number;
  variant: "success" | "warning" | "outline" | "destructive";
  entries: Array<{ name: string; label: string; detail?: string }>;
  emptyLabel: string;
}) {
  return (
    <div className="self-start rounded-lg border bg-muted/10 px-2.5 py-2 [@media(max-height:760px)]:px-2 [@media(max-height:760px)]:py-1.5">
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {title}
        </p>
        <Badge variant={variant}>{count}</Badge>
      </div>
      {entries.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {entries.map((entry) => (
            <div
              key={entry.name}
              className="rounded-md border bg-background/80 px-2 py-1 text-xs"
            >
              <span className="font-medium">{entry.label}</span>
              {entry.detail ? (
                <span className="text-muted-foreground"> · {entry.detail}</span>
              ) : null}
            </div>
          ))}
        </div>
      ) : (
        <p className="mt-2 text-xs text-muted-foreground">{emptyLabel}</p>
      )}
    </div>
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

  const oauthProviders = providers.filter(
    (provider) => provider.provider_type === "chatgpt_oauth",
  );
  const currentOAuthProvider = oauthProviders.find(
    (provider) => provider.name === currentProvider,
  );
  if (!currentOAuthProvider) return null;

  const statusByName = useMemo(
    () => new Map(statuses.map((status) => [status.provider.name, status])),
    [statuses],
  );

  const getAvailability = (provider: ProviderData): ChatGPTOAuthAvailability =>
    statusByName.get(provider.name)?.availability ??
    (provider.enabled ? "needs_sign_in" : "disabled");

  const allExtraProviders = oauthProviders.filter(
    (provider) => provider.name !== currentProvider,
  );
  const readyExtraProviders = allExtraProviders.filter(
    (provider) => getAvailability(provider) === "ready",
  );
  const mode = value.override_mode === "inherit" ? "inherit" : "custom";
  const providerDefaultsAvailable =
    defaultRouting != null && defaultRouting.extraProviderNames.length > 0;
  const selectedExtras = new Set(value.extra_provider_names ?? []);
  const selectedEntries = entries.map((entry) => ({
    ...entry,
    routeReadiness: getRouteReadiness(entry.availability, entry.quota),
    failureKind: getQuotaFailureKind(entry.quota),
  }));
  const healthyEntries = selectedEntries.filter(
    (entry) => entry.routeReadiness === "healthy",
  );
  const fallbackEntries = selectedEntries.filter(
    (entry) => entry.routeReadiness === "fallback",
  );
  const checkingEntries = selectedEntries.filter(
    (entry) => entry.routeReadiness === "checking",
  );
  const blockedEntries = selectedEntries.filter(
    (entry) => entry.routeReadiness === "blocked",
  );
  const routerActiveEntries = healthyEntries;
  const standbyEntries = [...fallbackEntries, ...checkingEntries];
  const selectedStrategy: EffectiveChatGPTOAuthRoutingStrategy =
    value.strategy === "round_robin" || value.strategy === "priority_order"
      ? value.strategy
      : "primary_first";
  const canEditMembership = canManageProviders && membershipEditable;
  const canUsePoolStrategies =
    canManageProviders &&
    mode !== "inherit" &&
    (membershipEditable || providerDefaultsAvailable || selectedEntries.length > 1);

  const setMode = (overrideMode: "inherit" | "custom") => {
    onChange({
      ...value,
      override_mode: overrideMode,
    });
  };

  const setStrategy = (strategy: EffectiveChatGPTOAuthRoutingStrategy) => {
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

  const routeDetail = (
    entry: (typeof selectedEntries)[number],
  ): string | undefined => {
    if (entry.availability !== "ready") {
      return t(`chatgptOAuthRouting.status.${entry.availability}`);
    }
    if (entry.failureKind) {
      return t(`chatgptOAuthRouting.quota.failure.${entry.failureKind}.label`);
    }
    if (entry.routeReadiness === "checking") {
      return t("chatgptOAuthRouting.quota.checking");
    }
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
            <div className="flex flex-wrap items-center gap-1.5">
              <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                {t("chatgptOAuthRouting.mode.label")}
              </p>
            </div>

            <div className="grid gap-1.5 xl:grid-cols-2">
              <Button
                type="button"
                variant={mode === "inherit" ? "default" : "outline"}
                onClick={() => setMode("inherit")}
                disabled={!canManageProviders || !providerDefaultsAvailable}
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
          <div className="flex flex-wrap items-center gap-1.5">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {t("chatgptOAuthRouting.strategyLabel")}
            </p>
          </div>

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

        <section className="space-y-2.5 [@media(max-height:760px)]:space-y-2">
          <div className="flex flex-wrap items-center gap-1.5">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {membershipEditable
                ? t("chatgptOAuthRouting.availableExtraAccountsLabel")
                : t("chatgptOAuthRouting.poolMembershipLabel")}
            </p>
          </div>

          {!membershipEditable ? (
            <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
              {selectedEntries.length > 1
                ? t("chatgptOAuthRouting.membershipManagedAtProvider", {
                    provider: membershipManagedByLabel || currentProvider,
                  })
                : t("chatgptOAuthRouting.membershipConfigureProviderFirst", {
                    provider: membershipManagedByLabel || currentProvider,
                  })}
            </div>
          ) : isLoading ? (
            <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.loadingAccounts")}
            </div>
          ) : readyExtraProviders.length > 0 ? (
            <div className="grid gap-2 xl:grid-cols-2">
              {readyExtraProviders.map((provider) => {
                const selected = selectedExtras.has(provider.name);
                const failureKind = getQuotaFailureKind(
                  quotaByName?.get(provider.name),
                );
                return (
                  <Button
                    key={provider.name}
                    type="button"
                    variant="outline"
                    size="sm"
                    className={cn(
                      "h-8 justify-start gap-1.5 rounded-lg px-3 text-left text-[13px] xl:h-9 xl:text-sm [@media(max-height:760px)]:h-8",
                      selected &&
                        "border-primary/40 bg-primary/10 text-foreground hover:bg-primary/15 dark:border-primary/30 dark:bg-primary/10",
                      !selected &&
                        failureKind &&
                        "border-amber-500/40 text-amber-700 dark:text-amber-300",
                      selected &&
                        failureKind &&
                        "border-amber-500/40 bg-amber-500/10 text-amber-900 hover:bg-amber-500/15 dark:text-amber-200",
                    )}
                    onClick={() => toggleProvider(provider.name)}
                    disabled={!canEditMembership || mode === "inherit"}
                  >
                    {selected ? <Check className="h-3.5 w-3.5" /> : null}
                    {provider.display_name || provider.name}
                  </Button>
                );
              })}
            </div>
          ) : (
            <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.noReadyExtras")}
            </div>
          )}
        </section>

        <section className="space-y-3 [@media(max-height:760px)]:space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-1.5">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {t("chatgptOAuthRouting.selectedAccountsLabel")}
            </p>
            <Badge variant="outline" className="h-6 px-2 text-[11px]">
              {t("chatgptOAuthRouting.selectedCount", {
                count: selectedEntries.length,
              })}
            </Badge>
          </div>

          {selectedEntries.length > 0 ? (
            <div className="rounded-lg border bg-muted/10 p-3 [@media(max-height:760px)]:p-2.5">
              <div className="grid gap-1.5 xl:grid-cols-2 [@media(max-height:760px)]:gap-1">
                {selectedEntries.map((entry) => (
                  <div
                    key={entry.name}
                    className="rounded-lg border bg-background/80 px-2.5 py-2 [@media(max-height:760px)]:px-2 [@media(max-height:760px)]:py-1.5"
                  >
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="min-w-0 truncate text-sm font-medium">
                        {entry.label}
                      </span>
                      <Badge
                        variant="outline"
                        className={cn(
                          "h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs",
                          roleBadgeClass(entry.role),
                        )}
                      >
                        {t(`chatgptOAuthRouting.role.${entry.role}`)}
                      </Badge>
                      {entry.availability !== "ready" && (
                        <Badge
                          variant={statusBadgeVariant(entry.availability)}
                          className="h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs"
                        >
                          {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                        </Badge>
                      )}
                      {entry.routeReadiness !== "healthy" && (
                        <Badge
                          variant={routeBadgeVariant(entry.routeReadiness)}
                          className="h-5 px-1.5 text-[10px] xl:h-6 xl:px-2 xl:text-xs"
                        >
                          {t(routeLabelKey(entry.routeReadiness))}
                        </Badge>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.emptySelected")}
            </div>
          )}
        </section>

        <section className="space-y-2.5 [@media(max-height:760px)]:space-y-2">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {t("chatgptOAuthRouting.poolStateTitle")}
          </p>

          <div className="grid items-start gap-1.5 xl:grid-cols-3 [@media(max-height:760px)]:gap-1">
            <StateGroup
              title={t("chatgptOAuthRouting.routerActiveTitle")}
              count={routerActiveEntries.length}
              variant="success"
              entries={routerActiveEntries.map((entry) => ({
                name: entry.name,
                label: entry.label,
                detail: routeDetail(entry),
              }))}
              emptyLabel={t("chatgptOAuthRouting.emptyGroup")}
            />
            <StateGroup
              title={t("chatgptOAuthRouting.fallbackTitle")}
              count={standbyEntries.length}
              variant="warning"
              entries={standbyEntries.map((entry) => ({
                name: entry.name,
                label: entry.label,
                detail: routeDetail(entry),
              }))}
              emptyLabel={t("chatgptOAuthRouting.emptyGroup")}
            />
            <StateGroup
              title={t("chatgptOAuthRouting.blockedNowTitle")}
              count={blockedEntries.length}
              variant="destructive"
              entries={blockedEntries.map((entry) => ({
                name: entry.name,
                label: entry.label,
                detail: routeDetail(entry),
              }))}
              emptyLabel={t("chatgptOAuthRouting.emptyGroup")}
            />
          </div>
        </section>
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
