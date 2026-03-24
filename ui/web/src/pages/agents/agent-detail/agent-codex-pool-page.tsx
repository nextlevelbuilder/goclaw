import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ArrowLeft, ShieldCheck } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { PageHeader } from "@/components/shared/page-header";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { ROUTES } from "@/lib/constants";
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
  } = useCodexPoolActivity(agent?.id ?? id, 18, Boolean(agent && isEligible));

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
  const failoverCount = activity.recent_requests.filter((request) => (request.failover_providers?.length ?? 0) > 0).length;
  const attentionEntries = liveEntries.filter((entry) => entry.availability !== "ready");
  const title = agent ? agentDisplayName(agent, t("card.unnamedAgent")) : "";
  const isDirty = savedRouting.strategy !== (routing.strategy === "round_robin" ? "round_robin" : "manual")
    || JSON.stringify(savedRouting.extraProviderNames) !== JSON.stringify(routing.extra_provider_names ?? []);
  const verdictTone = savedStrategy === "round_robin"
    ? readyCount > 1 && observedCount >= readyCount && activity.recent_requests.length > 1
      ? "healthy"
      : "warning"
    : "manual";

  if (loading || providersLoading || !agent) {
    return <DetailPageSkeleton tabs={0} />;
  }

  const metricCards = [
    {
      key: "health",
      label: t("chatgptOAuthRouting.metrics.poolHealth"),
      value: attentionEntries.length === 0 ? t("chatgptOAuthRouting.metrics.healthy") : t("chatgptOAuthRouting.metrics.needsAttention"),
      helper: t("chatgptOAuthRouting.metrics.readyOfTotal", { ready: readyCount, total: liveEntries.length }),
    },
    {
      key: "policy",
      label: t("chatgptOAuthRouting.metrics.policy"),
      value: savedStrategy === "round_robin" ? t("chatgptOAuthRouting.strategy.roundRobin") : t("chatgptOAuthRouting.strategy.manual"),
      helper: t("chatgptOAuthRouting.selectedAccountsLabel"),
    },
    {
      key: "ready",
      label: t("chatgptOAuthRouting.metrics.readyAccounts"),
      value: `${readyCount}/${liveEntries.length || 1}`,
      helper: t("chatgptOAuthRouting.recentRequestsCount", { count: activity.recent_requests.length }),
    },
    {
      key: "observed",
      label: t("chatgptOAuthRouting.metrics.observedRotation"),
      value: `${observedCount}/${liveEntries.length || 1}`,
      helper: failoverCount > 0
        ? t("chatgptOAuthRouting.metrics.failovers", { count: failoverCount })
        : t("chatgptOAuthRouting.metrics.noFailovers"),
    },
  ];

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
    <div className="p-4 sm:p-6 pb-10">
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
        <>
          {isDirty && (
            <Alert className="mt-4">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>{t("chatgptOAuthRouting.unsavedChangesTitle")}</AlertTitle>
              <AlertDescription>{t("chatgptOAuthRouting.unsavedChangesDescription")}</AlertDescription>
            </Alert>
          )}

          {!canManageProviders && (
            <Alert className="mt-4">
              <ShieldCheck className="h-4 w-4" />
              <AlertTitle>{t("chatgptOAuthRouting.providerAccessTitle")}</AlertTitle>
              <AlertDescription>{t("chatgptOAuthRouting.providerAccessDescription")}</AlertDescription>
            </Alert>
          )}

          <Alert className={`mt-4 ${verdictTone === "healthy" ? "border-emerald-500/30" : ""}`}>
            <ShieldCheck className={`h-4 w-4 ${verdictTone === "healthy" ? "text-emerald-600 dark:text-emerald-400" : ""}`} />
            <AlertTitle>{t(`chatgptOAuthRouting.verdict.${verdictTone}.title`)}</AlertTitle>
            <AlertDescription>{t(`chatgptOAuthRouting.verdict.${verdictTone}.description`, {
              observed: observedCount,
              ready: readyCount,
              count: activity.recent_requests.length,
            })}
            </AlertDescription>
          </Alert>

          <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            {metricCards.map((card) => (
              <Card key={card.key}>
                <CardContent className="space-y-1 pt-6">
                  <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{card.label}</p>
                  <div className="flex items-center gap-2">
                    <span className="text-lg font-semibold">{card.value}</span>
                    {card.key === "policy" && (
                      <Badge variant="outline">{currentProvider?.display_name || agent.provider}</Badge>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground">{card.helper}</p>
                </CardContent>
              </Card>
            ))}
          </div>

          <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1.45fr)_minmax(320px,0.95fr)]">
            <div className="space-y-4">
              <Alert>
                <ShieldCheck className="h-4 w-4" />
                <AlertTitle>{t("chatgptOAuthRouting.verificationTitle")}</AlertTitle>
                <AlertDescription>{t("chatgptOAuthRouting.verificationDescription")}</AlertDescription>
              </Alert>
              <CodexPoolActivityPanel
                entries={liveEntries}
                strategy={savedStrategy}
                recentRequests={activity.recent_requests}
                loading={activityLoading}
                fetching={activityFetching}
                showProviderLinks={canManageProviders}
                onRefresh={() => { void refreshActivity(); }}
              />
            </div>

            <div className="space-y-4">
              <ChatGPTOAuthRoutingSection
                currentProvider={agent.provider}
                providers={providers}
                value={routing}
                onChange={setRouting}
              />

              {attentionEntries.length > 0 ? (
                <Card>
                  <CardContent className="p-0">
                    <div className="border-b p-4">
                      <h3 className="text-sm font-medium">{t("chatgptOAuthRouting.needsAttentionTitle")}</h3>
                      <p className="mt-1 text-xs text-muted-foreground">{t("chatgptOAuthRouting.needsAttentionDescription", { count: attentionEntries.length })}</p>
                    </div>
                    <div className="divide-y">
                      {attentionEntries.map((entry) => (
                        <div key={entry.name} className="flex items-start justify-between gap-3 p-4">
                          <div className="space-y-1">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="text-sm font-medium">{entry.label}</span>
                              <Badge variant="outline">{t(`chatgptOAuthRouting.status.${entry.availability}`)}</Badge>
                            </div>
                            <p className="text-xs font-mono text-muted-foreground">{entry.name}</p>
                          </div>
                          {canManageProviders && (
                            <Button asChild variant="ghost" size="sm" className="h-8 shrink-0">
                              <Link to={ROUTES.PROVIDERS}>{t("chatgptOAuthRouting.openProviders")}</Link>
                            </Button>
                          )}
                        </div>
                      ))}
                    </div>
                  </CardContent>
                </Card>
              ) : liveEntries.length < 2 ? (
                <Alert>
                  <ShieldCheck className="h-4 w-4" />
                  <AlertTitle>{t("chatgptOAuthRouting.addPoolMembersTitle")}</AlertTitle>
                  <AlertDescription>{t("chatgptOAuthRouting.addPoolMembersDescription")}</AlertDescription>
                </Alert>
              ) : null}

              <Card>
                <CardContent className="space-y-3 pt-6">
                  <h3 className="text-sm font-medium">{t("chatgptOAuthRouting.howItWorksTitle")}</h3>
                  <ul className="space-y-2 text-sm text-muted-foreground">
                    <li>{t("chatgptOAuthRouting.howItWorks.primary")}</li>
                    <li>{t("chatgptOAuthRouting.howItWorks.roundRobin")}</li>
                    <li>{t("chatgptOAuthRouting.howItWorks.failover")}</li>
                  </ul>
                </CardContent>
              </Card>
            </div>
          </div>

          <StickySaveBar onSave={handleSave} saving={saving} disabled={!isDirty} />
        </>
      )}
    </div>
  );
}
