import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ArrowLeft, CheckCircle2, CircleDashed, GitFork, ShieldCheck } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { PageHeader } from "@/components/shared/page-header";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { ROUTES } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import {
  useChatGPTOAuthProviderStatuses,
  type ChatGPTOAuthAvailability,
} from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import { useAuthStore } from "@/stores/use-auth-store";
import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
import { useAgentDetail } from "../hooks/use-agent-detail";
import { buildAgentOtherConfigWithChatGPTOAuthRouting, agentDisplayName, normalizeChatGPTOAuthRouting } from "./agent-display-utils";
import { ChatGPTOAuthRoutingSection } from "./config-sections";
import { CodexPoolActivityPanel, type CodexPoolEntry } from "./codex-pool-activity-panel";
import { useCodexPoolActivity } from "./hooks/use-codex-pool-activity";

function providerStatus(
  providerName: string,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  enabled?: boolean,
): ChatGPTOAuthAvailability {
  return statusByName.get(providerName)?.availability ?? (enabled === false ? "disabled" : "needs_sign_in");
}

function ConfidenceStep({
  label,
  detail,
  tone,
}: {
  label: string;
  detail: string;
  tone: "done" | "warning" | "pending";
}) {
  return (
    <div className="h-full rounded-lg border bg-muted/20 p-3">
      <div className="flex items-start gap-2">
        {tone === "done" ? (
          <CheckCircle2 className="mt-0.5 h-4 w-4 text-emerald-600 dark:text-emerald-400" />
        ) : tone === "warning" ? (
          <AlertTriangle className="mt-0.5 h-4 w-4 text-amber-600 dark:text-amber-400" />
        ) : (
          <CircleDashed className="mt-0.5 h-4 w-4 text-muted-foreground" />
        )}
        <div className="min-w-0">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
          <p className="mt-1 text-sm font-medium">{detail}</p>
        </div>
      </div>
    </div>
  );
}

function MiniStat({
  label,
  value,
}: {
  label: string;
  value: string;
}) {
  return (
    <div className="rounded-lg border bg-muted/20 px-2.5 py-2">
      <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-1 text-base font-semibold leading-tight">{value}</p>
    </div>
  );
}

export function AgentCodexPoolPage() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation("agents");
  const role = useAuthStore((state) => state.role);
  const canManageProviders = role === "admin";
  const { agent, loading, updateAgent } = useAgentDetail(id);
  const { providers, loading: providersLoading } = useProviders();
  const { statuses } = useChatGPTOAuthProviderStatuses(providers);
  const savedRouting = useMemo(
    () => normalizeChatGPTOAuthRouting(agent?.other_config),
    [agent?.other_config],
  );
  const [routing, setRouting] = useState<ChatGPTOAuthRoutingConfig>({
    strategy: "manual",
    extra_provider_names: [],
  });
  const [saving, setSaving] = useState(false);
  const savedStrategy = savedRouting.strategy === "round_robin" ? "round_robin" : "manual";

  useEffect(() => {
    setRouting({
      strategy: savedRouting.strategy,
      extra_provider_names: savedRouting.extraProviderNames,
    });
  }, [savedRouting]);

  const providerByName = useMemo(
    () => new Map(providers.map((provider) => [provider.name, provider])),
    [providers],
  );
  const statusByName = useMemo(
    () => new Map(statuses.map((status) => [status.provider.name, status])),
    [statuses],
  );

  const currentProvider = agent ? providerByName.get(agent.provider) : undefined;
  const isEligible = Boolean(
    agent && (
      currentProvider?.provider_type === "chatgpt_oauth"
      || savedRouting.extraProviderNames.length > 0
      || savedRouting.strategy === "round_robin"
    ),
  );

  const {
    data: activity,
    isLoading: activityLoading,
    isFetching: activityFetching,
    refetch: refreshActivity,
  } = useCodexPoolActivity(agent?.id ?? id, 8, Boolean(agent && isEligible));

  const liveEntries = useMemo<CodexPoolEntry[]>(() => {
    if (!agent) return [];
    const countsByName = new Map(activity.provider_counts.map((item) => [item.provider_name, item]));
    const poolNames = [agent.provider, ...savedRouting.extraProviderNames].filter(Boolean);
    const uniqueNames = Array.from(new Set(poolNames));
    return uniqueNames.map((providerName) => {
      const provider = providerByName.get(providerName);
      const count = countsByName.get(providerName);
      return {
        name: providerName,
        label: provider?.display_name || providerName,
        availability: providerStatus(providerName, statusByName, provider?.enabled),
        role: providerName === agent.provider ? "preferred" : "extra",
        requestCount: count?.request_count ?? 0,
        lastUsedAt: count?.last_used_at,
        providerHref: provider?.id ? `/providers/${provider.id}` : undefined,
      };
    });
  }, [activity.provider_counts, agent, providerByName, savedRouting.extraProviderNames, statusByName]);

  const readyCount = liveEntries.filter((entry) => entry.availability === "ready").length;
  const observedCount = liveEntries.filter((entry) => entry.requestCount > 0).length;
  const observedReadyCount = liveEntries.filter((entry) => entry.availability === "ready" && entry.requestCount > 0).length;
  const failoverCount = activity.recent_requests.filter((request) => (request.failover_providers?.length ?? 0) > 0).length;
  const attentionEntries = liveEntries.filter((entry) => entry.availability !== "ready");
  const switchCount = activity.recent_requests.slice(1).reduce(
    (count, request, index) => count + (request.provider_name !== activity.recent_requests[index]?.provider_name ? 1 : 0),
    0,
  );
  const title = agent ? agentDisplayName(agent, t("card.unnamedAgent")) : "";
  const isDirty = savedRouting.strategy !== (routing.strategy === "round_robin" ? "round_robin" : "manual")
    || JSON.stringify(savedRouting.extraProviderNames) !== JSON.stringify(routing.extra_provider_names ?? []);
  const roundRobinVerified = savedStrategy === "round_robin"
    && readyCount > 1
    && observedReadyCount >= readyCount
    && switchCount >= Math.max(1, readyCount - 1);
  const showNextActionMeta = isDirty
    || attentionEntries.length > 0
    || !canManageProviders
    || (savedStrategy === "round_robin" && switchCount > 0);
  const lowerPanelHeightClass = "lg:h-full";

  const verdictTone = savedStrategy === "round_robin"
    ? roundRobinVerified
      ? "healthy"
      : "warning"
    : "manual";

  const nextAction = isDirty
    ? {
        title: t("chatgptOAuthRouting.nextAction.saveTitle"),
        description: t("chatgptOAuthRouting.nextAction.saveDescription"),
      }
    : attentionEntries.length > 0
      ? {
          title: t("chatgptOAuthRouting.nextAction.attentionTitle"),
          description: t("chatgptOAuthRouting.nextAction.attentionDescription"),
        }
      : liveEntries.length < 2
        ? {
            title: t("chatgptOAuthRouting.nextAction.addMembersTitle"),
            description: t("chatgptOAuthRouting.nextAction.addMembersDescription"),
          }
        : savedStrategy === "round_robin" && !roundRobinVerified
          ? {
              title: t("chatgptOAuthRouting.nextAction.verifyTitle"),
              description: t("chatgptOAuthRouting.nextAction.verifyDescription"),
            }
          : savedStrategy === "manual"
            ? {
                title: t("chatgptOAuthRouting.nextAction.manualTitle"),
                description: t("chatgptOAuthRouting.nextAction.manualDescription"),
              }
            : {
                title: t("chatgptOAuthRouting.nextAction.healthyTitle"),
                description: t("chatgptOAuthRouting.nextAction.healthyDescription"),
              };

  const confidenceSteps = [
    {
      label: t("chatgptOAuthRouting.checkpoints.configured"),
      detail: savedStrategy === "round_robin"
        ? t("chatgptOAuthRouting.strategy.roundRobin")
        : t("chatgptOAuthRouting.strategy.manual"),
      tone: "done" as const,
    },
    {
      label: t("chatgptOAuthRouting.checkpoints.ready"),
      detail: t("chatgptOAuthRouting.metrics.readyOfTotal", { ready: readyCount, total: liveEntries.length }),
      tone: readyCount === liveEntries.length && liveEntries.length > 0
        ? "done" as const
        : readyCount > 0
          ? "warning" as const
          : "pending" as const,
    },
    {
      label: t("chatgptOAuthRouting.checkpoints.observed"),
      detail: t("chatgptOAuthRouting.observedProviders", { observed: observedCount, total: liveEntries.length }),
      tone: observedCount >= liveEntries.length && liveEntries.length > 0
        ? "done" as const
        : observedCount > 0
          ? "warning" as const
          : "pending" as const,
    },
    {
      label: t("chatgptOAuthRouting.checkpoints.switching"),
      detail: savedStrategy === "manual"
        ? t("chatgptOAuthRouting.strategy.manual")
        : activity.recent_requests.length > 1
          ? t("chatgptOAuthRouting.switchRate", { switches: switchCount, total: activity.recent_requests.length - 1 })
          : t("chatgptOAuthRouting.recentRequestsCount", { count: activity.recent_requests.length }),
      tone: savedStrategy === "manual"
        ? "done" as const
        : roundRobinVerified
          ? "done" as const
          : switchCount > 0
            ? "warning" as const
            : "pending" as const,
    },
  ];

  if (loading || providersLoading || !agent) {
    return <DetailPageSkeleton tabs={0} />;
  }

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateAgent({
        other_config: buildAgentOtherConfigWithChatGPTOAuthRouting(agent, providers, routing),
      });
      await refreshActivity();
    } catch {
      // toast handled in hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex min-h-full flex-col p-4 pb-8 sm:p-6 lg:h-full lg:min-h-0">
      <Button variant="ghost" size="sm" className="mb-3 gap-1.5 px-0" onClick={() => navigate(`/agents/${agent.id}`)}>
        <ArrowLeft className="h-4 w-4" />
        {t("chatgptOAuthRouting.backToAgent")}
      </Button>

      <PageHeader
        title={t("chatgptOAuthRouting.pageTitle")}
        description={t("chatgptOAuthRouting.pageDescription", { name: title })}
        actions={canManageProviders ? (
          <Button asChild variant="outline" size="sm">
            <Link to={ROUTES.PROVIDERS}>{t("chatgptOAuthRouting.openProviders")}</Link>
          </Button>
        ) : undefined}
      />

      {!isEligible ? (
        <Alert className="mt-4">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>{t("chatgptOAuthRouting.pageUnsupportedTitle")}</AlertTitle>
          <AlertDescription>{t("chatgptOAuthRouting.pageUnsupportedDescription")}</AlertDescription>
        </Alert>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col">
          <Card className={cn(
            "mt-4 gap-4 overflow-hidden",
            verdictTone === "healthy" && "border-emerald-500/30",
            verdictTone === "warning" && "border-amber-500/30",
          )}>
            <CardHeader className="border-b bg-muted/20 px-6 py-4">
              <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
                <div className="space-y-2 xl:max-w-[680px]">
                  <div className="flex flex-wrap gap-2">
                    <Badge variant="outline">
                      {savedStrategy === "round_robin"
                        ? t("chatgptOAuthRouting.strategy.roundRobin")
                        : t("chatgptOAuthRouting.strategy.manual")}
                    </Badge>
                    {attentionEntries.length > 0 && (
                      <Badge variant="warning">{t("chatgptOAuthRouting.needsAttentionTitle")}</Badge>
                    )}
                    {failoverCount > 0 && (
                      <Badge variant="warning">{t("chatgptOAuthRouting.metrics.failovers", { count: failoverCount })}</Badge>
                    )}
                    {isDirty && <Badge variant="warning">{t("chatgptOAuthRouting.draftBadge")}</Badge>}
                  </div>

                  <div>
                    <CardTitle>{t(`chatgptOAuthRouting.verdict.${verdictTone}.title`)}</CardTitle>
                    <CardDescription>
                      {t(`chatgptOAuthRouting.verdict.${verdictTone}.description`, {
                        observed: observedCount,
                        ready: readyCount,
                        count: activity.recent_requests.length,
                      })}
                    </CardDescription>
                  </div>
                </div>

                <div className="grid gap-2 sm:grid-cols-3 xl:w-[400px]">
                  <MiniStat
                    label={t("chatgptOAuthRouting.metrics.policy")}
                    value={savedStrategy === "round_robin"
                      ? t("chatgptOAuthRouting.strategy.roundRobin")
                      : t("chatgptOAuthRouting.strategy.manual")}
                  />
                  <MiniStat
                    label={t("chatgptOAuthRouting.metrics.readyAccounts")}
                    value={`${readyCount}/${liveEntries.length || 1}`}
                  />
                  <MiniStat
                    label={t("chatgptOAuthRouting.metrics.observedRotation")}
                    value={`${observedCount}/${liveEntries.length || 1}`}
                  />
                </div>
              </div>
            </CardHeader>

            <CardContent className="space-y-3 px-6 pb-5 pt-4">
              <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(280px,0.9fr)]">
                <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                  {confidenceSteps.map((step) => (
                    <ConfidenceStep
                      key={step.label}
                      label={step.label}
                      detail={step.detail}
                      tone={step.tone}
                    />
                  ))}
                </div>

                <div className="flex h-full flex-col rounded-lg border bg-muted/20 p-3.5">
                  <div className="flex flex-1 flex-col gap-2.5">
                    <div className="flex flex-col gap-2 xl:flex-row xl:items-start xl:justify-between">
                      <div className="min-w-0 xl:flex-1">
                        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                            {t("chatgptOAuthRouting.nextActionTitle")}
                          </p>
                          <p className="text-sm font-medium leading-snug">{nextAction.title}</p>
                        </div>
                        <p className="mt-1 text-sm leading-snug text-muted-foreground">{nextAction.description}</p>
                      </div>

                      <div className="flex flex-wrap gap-2 xl:shrink-0 xl:justify-end">
                        {!isDirty && savedStrategy === "round_robin" && !roundRobinVerified && (
                          <Button type="button" variant="outline" size="sm" onClick={() => { void refreshActivity(); }}>
                            {t("chatgptOAuthRouting.refreshEvidence")}
                          </Button>
                        )}
                        {canManageProviders && (attentionEntries.length > 0 || liveEntries.length < 2) && (
                          <Button asChild type="button" variant="outline" size="sm">
                            <Link to={ROUTES.PROVIDERS}>{t("chatgptOAuthRouting.openProviders")}</Link>
                          </Button>
                        )}
                      </div>
                    </div>

                    {showNextActionMeta ? (
                      <div className="flex flex-wrap gap-x-3 gap-y-1.5 text-xs text-muted-foreground">
                        {isDirty && (
                          <div className="flex items-start gap-2">
                            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 text-amber-600 dark:text-amber-400" />
                            <span>{t("chatgptOAuthRouting.savedPoolOnlyHint")}</span>
                          </div>
                        )}
                        {attentionEntries.length > 0 && (
                          <div className="flex items-start gap-2">
                            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 text-amber-600 dark:text-amber-400" />
                            <span>{t("chatgptOAuthRouting.needsAttentionDescription", { count: attentionEntries.length })}</span>
                          </div>
                        )}
                        {!canManageProviders && (
                          <div className="flex items-start gap-2">
                            <ShieldCheck className="mt-0.5 h-3.5 w-3.5" />
                            <span>{t("chatgptOAuthRouting.providerAccessInline")}</span>
                          </div>
                        )}
                        {savedStrategy === "round_robin" && switchCount > 0 && (
                          <div className="flex items-start gap-2">
                            <GitFork className="mt-0.5 h-3.5 w-3.5" />
                            <span>{t("chatgptOAuthRouting.switchRate", { switches: switchCount, total: activity.recent_requests.length - 1 })}</span>
                          </div>
                        )}
                      </div>
                    ) : null}
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>

          <div className="mt-3 grid gap-4 lg:min-h-0 lg:flex-1 lg:grid-cols-[minmax(0,1.25fr)_minmax(320px,0.95fr)] lg:items-stretch">
            <ChatGPTOAuthRoutingSection
              currentProvider={agent.provider}
              providers={providers}
              value={routing}
              onChange={setRouting}
              canManageProviders={canManageProviders}
              className={lowerPanelHeightClass}
            />

            <CodexPoolActivityPanel
              entries={liveEntries}
              strategy={savedStrategy}
              recentRequests={activity.recent_requests}
              loading={activityLoading}
              fetching={activityFetching}
              showProviderLinks={canManageProviders}
              onRefresh={() => { void refreshActivity(); }}
              className={lowerPanelHeightClass}
            />
          </div>

          {isDirty ? (
            <StickySaveBar onSave={handleSave} saving={saving} disabled={!isDirty} />
          ) : null}
        </div>
      )}
    </div>
  );
}
