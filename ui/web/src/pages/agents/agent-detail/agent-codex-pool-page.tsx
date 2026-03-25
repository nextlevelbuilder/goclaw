import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ArrowLeft } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { ROUTES } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import {
  useChatGPTOAuthProviderStatuses,
  type ChatGPTOAuthAvailability,
} from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import { useChatGPTOAuthProviderQuotas } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import { useAuthStore } from "@/stores/use-auth-store";
import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
import { useAgentDetail } from "../hooks/use-agent-detail";
import {
  buildAgentOtherConfigWithChatGPTOAuthRouting,
  agentDisplayName,
  normalizeChatGPTOAuthRouting,
} from "./agent-display-utils";
import { getRouteReadiness } from "./chatgpt-oauth-quota-utils";
import { ChatGPTOAuthRoutingSection } from "./config-sections";
import {
  CodexPoolActivityPanel,
  type CodexPoolEntry,
} from "./codex-pool-activity-panel";
import { useCodexPoolActivity } from "./hooks/use-codex-pool-activity";

function providerStatus(
  providerName: string,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  enabled?: boolean,
): ChatGPTOAuthAvailability {
  return (
    statusByName.get(providerName)?.availability ??
    (enabled === false ? "disabled" : "needs_sign_in")
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
  const currentProvider = agent
    ? providerByName.get(agent.provider)
    : undefined;
  const isEligible = Boolean(
    agent &&
    (currentProvider?.provider_type === "chatgpt_oauth" ||
      savedRouting.extraProviderNames.length > 0 ||
      savedRouting.strategy === "round_robin"),
  );
  const quotaProviderNames = useMemo(
    () =>
      Array.from(
        new Set(
          [
            agent?.provider,
            ...savedRouting.extraProviderNames,
            ...(routing.extra_provider_names ?? []),
          ].filter(
            (providerName): providerName is string => {
              if (!providerName) return false;
              return (
                providerByName.get(providerName)?.provider_type ===
                "chatgpt_oauth"
              );
            },
          ),
        ),
      ),
    [
      agent?.provider,
      providerByName,
      routing.extra_provider_names,
      savedRouting.extraProviderNames,
    ],
  );
  const {
    quotaByName,
    isLoading: quotasLoading,
    isFetching: quotasFetching,
    refetch: refreshQuotas,
  } = useChatGPTOAuthProviderQuotas(
    quotaProviderNames,
    Boolean(agent && isEligible),
  );
  const {
    data: activity,
    isLoading: activityLoading,
    isFetching: activityFetching,
    refetch: refreshActivity,
  } = useCodexPoolActivity(agent?.id ?? id, 8, Boolean(agent && isEligible));

  const liveEntries = useMemo<CodexPoolEntry[]>(() => {
    if (!agent) return [];
    const countsByName = new Map(
      activity.provider_counts.map((item) => [item.provider_name, item]),
    );
    const poolNames = [
      agent.provider,
      ...savedRouting.extraProviderNames,
    ].filter(Boolean);
    const uniqueNames = Array.from(new Set(poolNames));
    return uniqueNames.map((providerName) => {
      const provider = providerByName.get(providerName);
      const count = countsByName.get(providerName);
      return {
        name: providerName,
        label: provider?.display_name || providerName,
        availability: providerStatus(
          providerName,
          statusByName,
          provider?.enabled,
        ),
        role: providerName === agent.provider ? "preferred" : "extra",
        requestCount: count?.request_count ?? 0,
        directSelectionCount:
          count?.direct_selection_count ?? count?.request_count ?? 0,
        failoverServeCount: count?.failover_serve_count ?? 0,
        lastSelectedAt: count?.last_selected_at,
        lastFailoverAt: count?.last_failover_at,
        lastUsedAt: count?.last_used_at,
        providerHref: provider?.id ? `/providers/${provider.id}` : undefined,
        quota: quotaByName.get(providerName),
      };
    });
  }, [
    activity.provider_counts,
    agent,
    providerByName,
    quotaByName,
    savedRouting.extraProviderNames,
    statusByName,
  ]);
  const draftEntries = useMemo<CodexPoolEntry[]>(() => {
    if (!agent) return [];
    const countsByName = new Map(
      activity.provider_counts.map((item) => [item.provider_name, item]),
    );
    const poolNames = [
      agent.provider,
      ...(routing.extra_provider_names ?? []),
    ].filter(Boolean);
    const uniqueNames = Array.from(new Set(poolNames));
    return uniqueNames.map((providerName) => {
      const provider = providerByName.get(providerName);
      const count = countsByName.get(providerName);
      return {
        name: providerName,
        label: provider?.display_name || providerName,
        availability: providerStatus(
          providerName,
          statusByName,
          provider?.enabled,
        ),
        role: providerName === agent.provider ? "preferred" : "extra",
        requestCount: count?.request_count ?? 0,
        directSelectionCount:
          count?.direct_selection_count ?? count?.request_count ?? 0,
        failoverServeCount: count?.failover_serve_count ?? 0,
        lastSelectedAt: count?.last_selected_at,
        lastFailoverAt: count?.last_failover_at,
        lastUsedAt: count?.last_used_at,
        providerHref: provider?.id ? `/providers/${provider.id}` : undefined,
        quota: quotaByName.get(providerName),
      };
    });
  }, [
    activity.provider_counts,
    agent,
    providerByName,
    quotaByName,
    routing.extra_provider_names,
    statusByName,
  ]);

  const routeEntries = useMemo(
    () =>
      liveEntries.map((entry) => ({
        ...entry,
        routeReadiness: getRouteReadiness(entry.availability, entry.quota),
      })),
    [liveEntries],
  );
  const healthyEntries = routeEntries.filter(
    (entry) => entry.routeReadiness === "healthy",
  );
  const routerActiveEntries = healthyEntries;
  const observedRouterActiveCount = routerActiveEntries.filter(
    (entry) => entry.directSelectionCount > 0,
  ).length;
  const switchCount = activity.recent_requests
    .slice(1)
    .reduce(
      (count, request, index) =>
        count +
        ((request.selected_provider || request.provider_name) !==
        (activity.recent_requests[index]?.selected_provider ||
          activity.recent_requests[index]?.provider_name)
          ? 1
          : 0),
      0,
    );
  const savedStrategy =
    savedRouting.strategy === "round_robin" ? "round_robin" : "manual";
  const isDirty =
    savedRouting.strategy !==
      (routing.strategy === "round_robin" ? "round_robin" : "manual") ||
    JSON.stringify(savedRouting.extraProviderNames) !==
      JSON.stringify(routing.extra_provider_names ?? []);
  const roundRobinVerified =
    savedStrategy === "round_robin" &&
    routerActiveEntries.length > 1 &&
    observedRouterActiveCount >= routerActiveEntries.length &&
    switchCount >= Math.max(1, routerActiveEntries.length - 1);
  const recentRequestCount = activity.recent_requests.length;
  const title = agent ? agentDisplayName(agent, t("card.unnamedAgent")) : "";

  if (loading || providersLoading || !agent) {
    return <DetailPageSkeleton tabs={0} />;
  }

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateAgent({
        other_config: buildAgentOtherConfigWithChatGPTOAuthRouting(
          agent,
          providers,
          routing,
        ),
      });
      await Promise.all([refreshActivity(), refreshQuotas()]);
    } catch {
      // toast handled in hook
    } finally {
      setSaving(false);
    }
  };

  const summaryTone =
    savedStrategy === "manual"
      ? "manual"
      : roundRobinVerified
        ? "healthy"
        : "warning";

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden p-4 sm:p-6">
      <Button
        variant="ghost"
        size="sm"
        className="mb-3 shrink-0 self-start gap-1.5 px-0"
        onClick={() => navigate(`/agents/${agent.id}`)}
      >
        <ArrowLeft className="h-4 w-4" />
        {t("chatgptOAuthRouting.backToAgent")}
      </Button>

      <div className="shrink-0">
        <PageHeader
          title={t("chatgptOAuthRouting.pageTitle")}
          description={t("chatgptOAuthRouting.pageDescription", { name: title })}
          actions={
            canManageProviders ? (
              <Button asChild variant="outline" size="sm">
                <Link to={ROUTES.PROVIDERS}>
                  {t("chatgptOAuthRouting.openProviders")}
                </Link>
              </Button>
            ) : undefined
          }
        />
      </div>

      {!isEligible ? (
        <Alert className="mt-4 shrink-0">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>
            {t("chatgptOAuthRouting.pageUnsupportedTitle")}
          </AlertTitle>
          <AlertDescription>
            {t("chatgptOAuthRouting.pageUnsupportedDescription")}
          </AlertDescription>
        </Alert>
      ) : (
        <div className="mt-4 flex min-h-0 flex-1 flex-col gap-4 overflow-hidden">
          <section
            className={cn(
              "shrink-0 rounded-xl border bg-card p-3",
              summaryTone === "healthy" && "border-emerald-500/30",
              summaryTone === "warning" && "border-amber-500/30",
            )}
          >
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="outline">
                {savedStrategy === "round_robin"
                  ? t("chatgptOAuthRouting.strategy.roundRobin")
                  : t("chatgptOAuthRouting.strategy.manual")}
              </Badge>
              {isDirty && (
                <Badge variant="warning">
                  {t("chatgptOAuthRouting.draftBadge")}
                </Badge>
              )}
            </div>
            <p className="mt-2 text-base font-semibold leading-snug">
              {t(`chatgptOAuthRouting.verdict.${summaryTone}.title`)}
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              {t(`chatgptOAuthRouting.verdict.${summaryTone}.description`, {
                observed: observedRouterActiveCount,
                count: recentRequestCount,
              })}
            </p>
          </section>

          <div className="grid min-h-0 flex-1 gap-4 overflow-y-auto overscroll-contain xl:grid-cols-[minmax(0,1.62fr)_minmax(320px,0.9fr)] xl:overflow-hidden">
            <CodexPoolActivityPanel
              entries={liveEntries}
              strategy={savedStrategy}
              recentRequests={activity.recent_requests}
              loading={activityLoading}
              fetching={activityFetching || quotasFetching}
              showProviderLinks={canManageProviders}
              onRefresh={() => {
                void Promise.all([refreshActivity(), refreshQuotas()]);
              }}
              className="h-full min-h-0"
            />

            <ChatGPTOAuthRoutingSection
              currentProvider={agent.provider}
              providers={providers}
              value={routing}
              onChange={setRouting}
              canManageProviders={canManageProviders}
              quotaByName={quotaByName}
              quotaLoading={quotasLoading || quotasFetching}
              entries={draftEntries}
              isDirty={isDirty}
              saving={saving}
              onSave={handleSave}
              className="h-full min-h-0"
            />
          </div>
        </div>
      )}
    </div>
  );
}
