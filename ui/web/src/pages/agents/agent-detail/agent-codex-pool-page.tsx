import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useChatGPTOAuthProviderStatuses } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import { useChatGPTOAuthProviderQuotas } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import { useAuthStore } from "@/stores/use-auth-store";
import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
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
} from "./agent-display-utils";
import { getRouteReadiness } from "./chatgpt-oauth-quota-utils";
import { ChatGPTOAuthRoutingSection } from "./config-sections";
import { CodexPoolActivityPanel } from "./codex-pool-activity-panel";
import { CodexPoolPageHeader } from "./codex-pool-page-header";
import {
  buildDraftRouting,
  routingDraftSignature,
} from "./codex-pool-routing-draft-utils";
import { useCodexPoolActivity } from "./hooks/use-codex-pool-activity";
import { toPoolEntriesMerged } from "@/adapters/provider-pool.adapter";

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
    () => new Map(providers.map((p) => [p.name, p])),
    [providers],
  );
  const statusByName = useMemo(
    () => new Map(statuses.map((s) => [s.provider.name, s])),
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
    () => normalizeChatGPTOAuthRouting(agent?.chatgpt_oauth_routing ?? agent?.other_config),
    [agent?.chatgpt_oauth_routing, agent?.other_config],
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
    () => buildDraftRouting(savedRouting),
    [savedRouting],
  );
  const savedDraftSignature = useMemo(
    () => routingDraftSignature(savedDraftRouting),
    [savedDraftRouting],
  );

  const [routing, setRouting] = useState<ChatGPTOAuthRoutingConfig>(savedDraftRouting);
  const [saving, setSaving] = useState(false);
  const syncedAgentIDRef = useRef(agent?.id ?? "");
  const savedDraftSignatureRef = useRef(savedDraftSignature);

  const draftSignature = useMemo(
    () => routingDraftSignature(routing),
    [routing],
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
    if (savedDraftSignature === previousSavedSignature) return;
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
            (name): name is string =>
              Boolean(name) &&
              providerByName.get(name)?.provider_type === "chatgpt_oauth",
          ),
        ),
      ),
    [
      draftEffectiveRouting.poolProviderNames,
      providerByName,
      savedEffectiveRouting.poolProviderNames,
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
    isFetching: activityFetching,
    refetch: refreshActivity,
  } = useCodexPoolActivity(agent?.id ?? id, 8, Boolean(agent && isEligible));

  const liveEntries = useMemo(
    () => !agent ? [] : toPoolEntriesMerged(savedEffectiveRouting.poolProviderNames, activity.provider_counts, agent.provider, providerByName, statusByName, quotaByName),
    [activity.provider_counts, agent, providerByName, quotaByName, savedEffectiveRouting.poolProviderNames, statusByName],
  );
  const draftEntries = useMemo(
    () => !agent ? [] : toPoolEntriesMerged(draftEffectiveRouting.poolProviderNames, activity.provider_counts, agent.provider, providerByName, statusByName, quotaByName),
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
  const readyEntries = liveEntries.filter((e) => e.availability === "ready");
  const runtimeHealthyEntries = liveEntries.filter((e) => e.healthState === "healthy");
  const runtimeDegradedEntries = liveEntries.filter((e) => e.healthState === "degraded");
  const runtimeCriticalEntries = liveEntries.filter((e) => e.healthState === "critical");
  const blockedEntries = routeEntries.filter((e) => e.routeReadiness === "blocked");

  const observedRoutableCount = routeEntries.filter(
    (e) => e.routeReadiness !== "blocked" && e.directSelectionCount > 0,
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

  const summaryTone =
    savedStrategy !== "round_robin"
      ? "manual"
      : roundRobinVerified
        ? "healthy"
        : "warning";

  if (loading || providersLoading || !agent) {
    return <DetailPageSkeleton tabs={0} />;
  }

  const handleSave = async () => {
    setSaving(true);
    try {
      // buildAgentOtherConfigWithChatGPTOAuthRouting returns top-level fields
      const payload = buildAgentOtherConfigWithChatGPTOAuthRouting(
        agent,
        routing,
        currentProvider?.settings,
      );
      await updateAgent(payload);
      await Promise.all([refreshActivity(), refreshQuotas()]);
    } catch {
      // toast handled in hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden p-3 sm:p-4 xl:p-5 [@media(max-height:760px)]:p-2.5">
      <CodexPoolPageHeader
        title={agentDisplayName(agent, t("card.unnamedAgent"))}
        savedStrategy={savedStrategy}
        summaryTone={summaryTone}
        overrideMode={savedEffectiveRouting.overrideMode}
        recentRequestCount={activity.stats_sample_size ?? 0}
        runtimeHealthyCount={runtimeHealthyEntries.length}
        runtimeDegradedCount={runtimeDegradedEntries.length}
        runtimeCriticalCount={runtimeCriticalEntries.length}
        isDirty={isDirty}
        canManageProviders={canManageProviders}
        isEligible={isEligible}
        onBack={() => navigate(`/agents/${agent.id}`)}
      />

      {isEligible ? (
        <div className="mt-2 flex min-h-0 flex-1 flex-col gap-3 overflow-hidden [@media(max-height:760px)]:gap-2">
          <div className="grid min-h-0 flex-1 gap-3 overflow-y-auto overscroll-contain lg:grid-cols-[minmax(0,1.45fr)_minmax(320px,0.95fr)] lg:items-start lg:overflow-hidden [@media(max-height:760px)]:gap-2">
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

            <div className="flex min-h-0 flex-col gap-4 overflow-hidden lg:h-full lg:self-stretch">
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
                membershipEditable={false}
                membershipManagedByLabel={
                  currentProvider?.display_name || agent.provider
                }
                quotaByName={quotaByName}
                quotaLoading={quotasLoading || quotasFetching}
                entries={draftEntries}
                isDirty={isDirty}
                saving={saving}
                onSave={handleSave}
                contentScrollable
                className="h-full min-h-0"
              />
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
