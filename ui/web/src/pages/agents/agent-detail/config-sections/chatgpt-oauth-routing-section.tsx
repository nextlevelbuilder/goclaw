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
import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
import type { ProviderData } from "@/types/provider";
import type { CodexPoolEntry } from "../codex-pool-activity-panel";
import {
  getQuotaFailureKind,
  getRouteReadiness,
} from "../chatgpt-oauth-quota-utils";

interface ChatGPTOAuthRoutingSectionProps {
  currentProvider: string;
  providers: ProviderData[];
  value: ChatGPTOAuthRoutingConfig;
  onChange: (value: ChatGPTOAuthRoutingConfig) => void;
  canManageProviders?: boolean;
  quotaByName?: Map<string, ChatGPTOAuthProviderQuota>;
  quotaLoading?: boolean;
  entries?: CodexPoolEntry[];
  isDirty?: boolean;
  saving?: boolean;
  onSave?: () => void;
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
    <div className="self-start rounded-lg border bg-muted/10 px-3 py-2.5">
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
  currentProvider,
  providers,
  value,
  onChange,
  canManageProviders = true,
  quotaByName,
  quotaLoading = false,
  entries = [],
  isDirty = false,
  saving = false,
  onSave,
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
    <Card className={cn("flex h-full min-h-0 flex-col gap-0 overflow-hidden", className)}>
      <CardHeader className="border-b bg-muted/20 px-4 py-3">
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <CardTitle className="text-base">
                {t("chatgptOAuthRouting.controlTitle")}
              </CardTitle>
              <CardDescription>
                {t("chatgptOAuthRouting.controlDescription")}
              </CardDescription>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {!canManageProviders && (
                <Badge variant="outline">
                  {t("chatgptOAuthRouting.viewerMode")}
                </Badge>
              )}
              {isDirty && (
                <Badge variant="warning">
                  {t("chatgptOAuthRouting.draftBadge")}
                </Badge>
              )}
              {(quotaLoading || isLoading) && (
                <Badge variant="outline">
                  {t("chatgptOAuthRouting.quota.checking")}
                </Badge>
              )}
              <Button
                type="button"
                size="sm"
                onClick={onSave}
                disabled={!canManageProviders || !isDirty || saving}
              >
                {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                {saving ? tc("saving") : tc("save")}
              </Button>
            </div>
          </div>
        </div>
      </CardHeader>

      <CardContent className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain px-4 py-3">
        <section className="space-y-3">
          <div className="flex items-center justify-between gap-2">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
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
              disabled={!canManageProviders}
            >
              {t("chatgptOAuthRouting.strategy.manual")}
            </Button>
            <Button
              type="button"
              variant={value.strategy === "round_robin" ? "default" : "outline"}
              onClick={() => setStrategy("round_robin")}
              disabled={!canManageProviders}
            >
              {t("chatgptOAuthRouting.strategy.roundRobin")}
            </Button>
          </div>
        </section>

        <section className="space-y-3">
          <div className="flex items-center justify-between gap-2">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {t("chatgptOAuthRouting.availableExtraAccountsLabel")}
            </p>
            <Badge variant="outline">
              {t("chatgptOAuthRouting.readySummary", {
                ready: readyExtraProviders.length,
                total: allExtraProviders.length,
              })}
            </Badge>
          </div>

          {isLoading ? (
            <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.loadingAccounts")}
            </div>
          ) : readyExtraProviders.length > 0 ? (
            <div className="grid gap-2 sm:grid-cols-2">
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
                      "h-10 justify-start gap-1.5 rounded-lg px-3 text-left",
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
                    disabled={!canManageProviders}
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

        <section className="space-y-3">
          <div className="flex items-center justify-between gap-2">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {t("chatgptOAuthRouting.selectedAccountsLabel")}
            </p>
            <Badge variant="outline">
              {t("chatgptOAuthRouting.selectedCount", {
                count: selectedEntries.length,
              })}
            </Badge>
          </div>

          {selectedEntries.length > 0 ? (
            <div className="rounded-lg border bg-muted/10 p-3">
              <div className="grid gap-2 sm:grid-cols-2">
                {selectedEntries.map((entry) => (
                  <div
                    key={entry.name}
                    className="rounded-lg border bg-background/80 px-3 py-2"
                  >
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-medium">{entry.label}</span>
                      <Badge
                        variant={
                          entry.role === "preferred" ? "secondary" : "outline"
                        }
                      >
                        {t(`chatgptOAuthRouting.role.${entry.role}`)}
                      </Badge>
                      <Badge variant={statusBadgeVariant(entry.availability)}>
                        {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                      </Badge>
                      <Badge variant={routeBadgeVariant(entry.routeReadiness)}>
                        {t(routeLabelKey(entry.routeReadiness))}
                      </Badge>
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

        <section className="space-y-3">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {t("chatgptOAuthRouting.poolStateTitle")}
          </p>

          <div className="grid items-start gap-2 xl:grid-cols-3">
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
    </Card>
  );
}
