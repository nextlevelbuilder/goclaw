import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ArrowLeft } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { PageHeader } from "@/components/shared/page-header";
import { ROUTES } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import {
  useChatGPTOAuthProviderStatuses,
  type ChatGPTOAuthAvailability,
} from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import { useChatGPTOAuthProviderQuotas } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import { useAuthStore } from "@/stores/use-auth-store";
import type {
  ChatGPTOAuthRoutingConfig,
  EffectiveChatGPTOAuthRoutingStrategy,
} from "@/types/agent";
import {
  getChatGPTOAuthProviderRouting,
  normalizeChatGPTOAuthStrategy,
} from "@/types/provider";
import { useAgentDetail } from "../hooks/use-agent-detail";
import {
  agentDisplayName,
  buildAgentOtherConfigWithChatGPTOAuthRouting,
  normalizeChatGPTOAuthRouting,
  normalizeChatGPTOAuthRoutingInput,
  resolveEffectiveChatGPTOAuthRouting,
  type NormalizedChatGPTOAuthRouting,
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

function strategyLabelKey(
  strategy: EffectiveChatGPTOAuthRoutingStrategy,
): string {
  if (strategy === "round_robin") return "chatgptOAuthRouting.strategy.roundRobin";
  if (strategy === "priority_order") return "chatgptOAuthRouting.strategy.priorityOrder";
  return "chatgptOAuthRouting.strategy.primaryFirst";
}

function buildDraftRouting(
  savedRouting: NormalizedChatGPTOAuthRouting,
  hasProviderDefaults: boolean,
): ChatGPTOAuthRoutingConfig {
  if (savedRouting.isExplicit) {
    return {
      override_mode: savedRouting.overrideMode,
      strategy: savedRouting.strategy,
      extra_provider_names: savedRouting.extraProviderNames,
    };
  }

  if (hasProviderDefaults) {
    return {
      override_mode: "inherit",
      strategy: "primary_first",
      extra_provider_names: [],
    };
  }

  return {
    override_mode: "custom",
    strategy: "primary_first",
    extra_provider_names: [],
  };
}

function routingDraftSignature(
  routing: ChatGPTOAuthRoutingConfig,
  hasProviderDefaults: boolean,
): string {
  const normalized = normalizeChatGPTOAuthRoutingInput(routing);
  if (normalized.overrideMode === "inherit" && hasProviderDefaults) {
    return JSON.stringify({ override_mode: "inherit" });
  }
  return JSON.stringify({
    override_mode: "custom",
    strategy: normalized.strategy,
    extra_provider_names: normalized.extraProviderNames,
  });
}

export function AgentCodexPoolPage() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation("agents");
  const role = useAuthStore((state) => state.role);
  const canManageProviders = role === "admin" || role === "owner";
  const { agent, loading, updateAgent } = useAgentDetail(id);
  const { providers, loading: providersLoading } = useProviders();
  const { statuses } = useChatGPTOAuthProviderStatuses(providers);
  const providerByName = useMemo(
    () => new Map(providers.map((provider) => [provider.name, provider])),
    [providers],
  );
  const statusByName = useMemo(
    () => new Map(statuses.map((status) => [status.provider.name, status])),
    [statuses],
  );
  const currentProvider = agent ? providerByName.get(agent.provider) : undefined;
  const providerDefaults = useMemo(
    () => getChatGPTOAuthProviderRouting(currentProvider?.settings),
    [currentProvider?.settings],
  );
  const isEligible = Boolean(
    agent && currentProvider?.provider_type === "chatgpt_oauth",
  );
  const savedRouting = useMemo(
    () => normalizeChatGPTOAuthRouting(agent?.other_config),
    [agent?.other_config],
  );
  const savedEffectiveRouting = useMemo(
    () =>
      resolveEffectiveChatGPTOAuthRouting(
        agent?.provider ?? "",
        currentProvider?.settings,
        savedRouting,
      ),
    [agent?.provider, currentProvider?.settings, savedRouting],
  );
  const savedDraftRouting = useMemo(
    () => buildDraftRouting(savedRouting, Boolean(providerDefaults)),
    [providerDefaults, savedRouting],
  );
  const savedDraftSignature = useMemo(
    () => routingDraftSignature(savedDraftRouting, Boolean(providerDefaults)),
    [providerDefaults, savedDraftRouting],
  );
  const [routing, setRouting] = useState<ChatGPTOAuthRoutingConfig>(savedDraftRouting);
  const [saving, setSaving] = useState(false);
  const syncedAgentIDRef = useRef(agent?.id ?? "");
  const savedDraftSignatureRef = useRef(savedDraftSignature);

  const draftSignature = useMemo(
    () => routingDraftSignature(routing, Boolean(providerDefaults)),
    [providerDefaults, routing],
  );

  useEffect(() => {
    const nextAgentID = agent?.id ?? "";
    if (nextAgentID !== syncedAgentIDRef.current) {
      syncedAgentIDRef.current = nextAgentID;
      savedDraftSignatureRef.current = savedDraftSignature;
      setRouting(savedDraftRouting);
      return;
    }

    const previousSavedSignature = savedDraftSignatureRef.current;
    if (savedDraftSignature === previousSavedSignature) {
      return;
    }

    if (draftSignature === previousSavedSignature) {
      setRouting(savedDraftRouting);
    }
    savedDraftSignatureRef.current = savedDraftSignature;
  }, [agent?.id, draftSignature, savedDraftRouting, savedDraftSignature]);

  const draftRouting = useMemo(
    () => normalizeChatGPTOAuthRoutingInput(routing),
    [routing],
  );
  const draftEffectiveRouting = useMemo(
    () =>
      resolveEffectiveChatGPTOAuthRouting(
        agent?.provider ?? "",
        currentProvider?.settings,
        draftRouting,
      ),
    [agent?.provider, currentProvider?.settings, draftRouting],
  );
  const quotaProviderNames = useMemo(
    () =>
      Array.from(
        new Set(
          [
            ...savedEffectiveRouting.poolProviderNames,
            ...draftEffectiveRouting.poolProviderNames,
          ].filter(
            (providerName): providerName is string =>
              Boolean(providerName) &&
              providerByName.get(providerName)?.provider_type === "chatgpt_oauth",
          ),
        ),
      ),
    [draftEffectiveRouting.poolProviderNames, providerByName, savedEffectiveRouting.poolProviderNames],
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
    isFetching: activityFetching,
    refetch: refreshActivity,
  } = useCodexPoolActivity(agent?.id ?? id, 8, Boolean(agent && isEligible));

  const buildEntries = (
    poolNames: string[],
  ): CodexPoolEntry[] => {
    if (!agent) return [];
    const countsByName = new Map(
      activity.provider_counts.map((item) => [item.provider_name, item]),
    );

    return poolNames.map((providerName) => {
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
        successCount: count?.success_count ?? 0,
        failureCount: count?.failure_count ?? 0,
        consecutiveFailures: count?.consecutive_failures ?? 0,
        successRate: count?.success_rate ?? 0,
        healthScore: count?.health_score ?? 0,
        healthState: count?.health_state ?? "idle",
        lastSelectedAt: count?.last_selected_at,
        lastFailoverAt: count?.last_failover_at,
        lastUsedAt: count?.last_used_at,
        lastSuccessAt: count?.last_success_at,
        lastFailureAt: count?.last_failure_at,
        providerHref: provider?.id ? `/providers/${provider.id}` : undefined,
        quota: quotaByName.get(providerName),
      };
    });
  };

  const liveEntries = useMemo(
    () => buildEntries(savedEffectiveRouting.poolProviderNames),
    [activity.provider_counts, agent, providerByName, quotaByName, savedEffectiveRouting.poolProviderNames, statusByName],
  );
  const draftEntries = useMemo(
    () => buildEntries(draftEffectiveRouting.poolProviderNames),
    [activity.provider_counts, agent, draftEffectiveRouting.poolProviderNames, providerByName, quotaByName, statusByName],
  );

  const routeEntries = useMemo(
    () =>
      liveEntries.map((entry) => ({
        ...entry,
        routeReadiness: getRouteReadiness(entry.availability, entry.quota),
      })),
    [liveEntries],
  );
  const blockedEntries = routeEntries.filter(
    (entry) => entry.routeReadiness === "blocked",
  );
  const readyEntries = liveEntries.filter(
    (entry) => entry.availability === "ready",
  );
  const runtimeHealthyEntries = liveEntries.filter(
    (entry) => entry.healthState === "healthy",
  );
  const runtimeDegradedEntries = liveEntries.filter(
    (entry) => entry.healthState === "degraded",
  );
  const runtimeCriticalEntries = liveEntries.filter(
    (entry) => entry.healthState === "critical",
  );
  const observedRoutableCount = routeEntries.filter(
    (entry) =>
      entry.routeReadiness !== "blocked" && entry.directSelectionCount > 0,
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
  const savedStrategy = normalizeChatGPTOAuthStrategy(
    activity.strategy || savedEffectiveRouting.strategy,
  );
  const isDirty = draftSignature !== savedDraftSignature;
  const roundRobinVerified =
    savedStrategy === "round_robin" &&
    readyEntries.length > 1 &&
    observedRoutableCount >= readyEntries.length &&
    switchCount >= Math.max(1, readyEntries.length - 1) &&
    blockedEntries.length === 0 &&
    runtimeCriticalEntries.length === 0;
  const recentRequestCount = activity.stats_sample_size ?? 0;
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
          routing,
          currentProvider?.settings,
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
    savedStrategy !== "round_robin"
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
                {t(strategyLabelKey(savedStrategy))}
              </Badge>
              <Badge variant="outline">
                {savedEffectiveRouting.overrideMode === "inherit"
                  ? t("chatgptOAuthRouting.mode.inherit")
                  : t("chatgptOAuthRouting.mode.custom")}
              </Badge>
              <Badge variant="outline">
                {recentRequestCount > 0
                  ? t("chatgptOAuthRouting.sampleBadge", {
                      count: recentRequestCount,
                    })
                  : t("chatgptOAuthRouting.noSampleBadge")}
              </Badge>
              {isDirty ? (
                <Badge variant="warning">
                  {t("chatgptOAuthRouting.draftBadge")}
                </Badge>
              ) : null}
              <Badge variant="success">
                {t("chatgptOAuthRouting.healthState.healthy")}{" "}
                {runtimeHealthyEntries.length}
              </Badge>
              {runtimeDegradedEntries.length > 0 ? (
                <Badge variant="warning">
                  {t("chatgptOAuthRouting.healthState.degraded")}{" "}
                  {runtimeDegradedEntries.length}
                </Badge>
              ) : null}
              {runtimeCriticalEntries.length > 0 ? (
                <Badge variant="destructive">
                  {t("chatgptOAuthRouting.healthState.critical")}{" "}
                  {runtimeCriticalEntries.length}
                </Badge>
              ) : null}
            </div>
            <p className="mt-2 text-base font-semibold leading-snug">
              {t(`chatgptOAuthRouting.verdict.${summaryTone}.title`)}
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              {t(`chatgptOAuthRouting.verdict.${summaryTone}.description`, {
                observed: observedRoutableCount,
                count: recentRequestCount,
              })}
            </p>
          </section>

          <div className="grid min-h-0 flex-1 gap-4 overflow-y-auto overscroll-contain xl:grid-cols-[minmax(0,1.45fr)_minmax(360px,0.95fr)] xl:items-start xl:overflow-hidden">
            <CodexPoolActivityPanel
              entries={liveEntries}
              strategy={savedStrategy}
              recentRequests={activity.recent_requests}
              statsSampleSize={activity.stats_sample_size ?? 0}
              fetching={activityFetching}
              showProviderLinks={canManageProviders}
              onRefresh={() => {
                void Promise.all([refreshActivity(), refreshQuotas()]);
              }}
              className="h-full min-h-0"
            />

            <div className="flex min-h-0 flex-col gap-4 overflow-hidden xl:h-full xl:self-stretch">
              <ChatGPTOAuthRoutingSection
                currentProvider={agent.provider}
                providers={providers}
                value={routing}
                onChange={setRouting}
                defaultRouting={
                  providerDefaults
                    ? {
                        strategy: providerDefaults.strategy,
                        extraProviderNames: providerDefaults.extraProviderNames,
                      }
                    : null
                }
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
        </div>
      )}
    </div>
  );
}
