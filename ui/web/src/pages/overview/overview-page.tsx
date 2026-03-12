import { useEffect, useCallback, lazy, Suspense } from "react";
import { Activity, Bot, DollarSign, Hash, Radio, AlertTriangle } from "lucide-react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shared/page-header";
import { StatusBadge } from "@/components/shared/status-badge";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { useAuthStore } from "@/stores/use-auth-store";
import { useWsCall } from "@/hooks/use-ws-call";
import { useWsEvent } from "@/hooks/use-ws-event";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useTraces } from "@/pages/traces/hooks/use-traces";
import { Methods, Events } from "@/api/protocol";
import { ROUTES } from "@/lib/constants";
import { formatTokens, formatCost } from "@/lib/format";

import type {
  HealthPayload,
  StatusPayload,
  QuotaUsageResult,
  CronListPayload,
  ChannelStatusPayload,
} from "./types";
import { useLiveUptime } from "./hooks/use-live-uptime";
import { StatCard } from "./stat-card";
import { useOverviewSparklines } from "./hooks/use-overview-sparklines";
import { SystemHealthCard } from "./system-health-card";
import { ConnectedClientsCard } from "./connected-clients-card";
import { CronJobsCard } from "./cron-jobs-card";
import { RecentRequestsCard } from "./recent-requests-card";
import { QuotaUsageCard } from "./quota-usage-card";
import { useRuntimes } from "@/pages/skills/hooks/use-runtimes";

const UsagePage = lazy(() =>
  import("@/pages/usage/usage-page").then((m) => ({ default: m.UsagePage })),
);

const REFRESH_INTERVAL = 30_000;

export function OverviewPage() {
  const { t } = useTranslation("overview");
  const connected = useAuthStore((s) => s.connected);
  const { call: fetchHealth, data: health } =
    useWsCall<HealthPayload>(Methods.HEALTH);
  const { call: fetchStatus, data: status } =
    useWsCall<StatusPayload>(Methods.STATUS);
  const { call: fetchQuota, data: quota } =
    useWsCall<QuotaUsageResult>(Methods.QUOTA_USAGE);
  const sparklines = useOverviewSparklines();
  const { call: fetchCron, data: cronData } =
    useWsCall<CronListPayload>(Methods.CRON_LIST);
  const { call: fetchChannels, data: channelStatusData } =
    useWsCall<ChannelStatusPayload>(Methods.CHANNELS_STATUS);
  const { providers, loading: providersLoading } = useProviders();
  const { runtimes } = useRuntimes();
  const { traces } = useTraces({ limit: 8 });

  const hasNoProviders = !providersLoading && providers.length === 0;
  const hasNoEnabledProviders =
    !providersLoading &&
    providers.length > 0 &&
    !providers.some((p) => p.enabled);

  const fetchAll = useCallback(() => {
    fetchHealth();
    fetchStatus();
    fetchQuota();
    fetchCron({ includeDisabled: true });
    fetchChannels();
  }, [fetchHealth, fetchStatus, fetchQuota, fetchCron, fetchChannels]);

  useEffect(() => {
    if (!connected) return;
    fetchAll();
    const id = setInterval(fetchAll, REFRESH_INTERVAL);
    return () => clearInterval(id);
  }, [connected, fetchAll]);

  const handleHealthEvent = useCallback(() => {
    fetchHealth();
    fetchStatus();
  }, [fetchHealth, fetchStatus]);
  useWsEvent(Events.HEALTH, handleHealthEvent);

  const liveUptime = useLiveUptime(health?.uptime);

  // Computed
  const agents = status?.agents ?? [];
  const runningAgents = agents.filter((a) => a.isRunning).length;
  const agentTotal = status?.agentTotal ?? agents.length;
  const channelEntries = channelStatusData?.channels
    ? Object.entries(channelStatusData.channels)
    : [];
  const channelsOnline = channelEntries.filter(([, c]) => c.running).length;
  const enabledProviders = providers.filter((p) => p.enabled);
  const clientList = health?.clients ?? [];

  return (
    <div className="space-y-6 p-4 sm:p-6">
      {/* Header */}
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex items-center gap-2">
            {health?.version && (
              <span className="text-xs text-muted-foreground">
                v{health.version}
              </span>
            )}
            <StatusBadge
              status={connected ? "success" : "error"}
              label={connected ? t("common:connected", "Connected") : t("common:disconnected", "Disconnected")}
            />
          </div>
        }
      />

      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">{t("tabs.overview")}</TabsTrigger>
          <TabsTrigger value="usage">{t("tabs.usage")}</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-6">
          {/* Provider warning */}
          {(hasNoProviders || hasNoEnabledProviders) && (
            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>
                {hasNoProviders
                  ? t("providers.noProvidersTitle")
                  : t("providers.noEnabledTitle")}
              </AlertTitle>
              <AlertDescription>
                {hasNoProviders
                  ? t("providers.noProvidersDesc")
                  : t("providers.noEnabledDesc")}
                <Link
                  to={ROUTES.PROVIDERS}
                  className="font-medium underline underline-offset-4 hover:text-foreground"
                >
                  {t("providers.goToSettings")}
                </Link>
              </AlertDescription>
            </Alert>
          )}

          {/* Summary cards */}
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
            <StatCard
              icon={Activity}
              label={t("statCards.requestsToday")}
              value={quota?.requestsToday ?? 0}
              sub={
                quota?.uniqueUsersToday
                  ? t("statCards.users", { count: quota.uniqueUsersToday })
                  : undefined
              }
              sparkline={sparklines?.requestSparkline}
              trend={sparklines?.trends.requests}
            />
            <StatCard
              icon={Hash}
              label={t("statCards.tokensToday")}
              value={formatTokens(
                (quota?.inputTokensToday ?? 0) + (quota?.outputTokensToday ?? 0),
              )}
              sub={
                quota
                  ? t("statCards.inOut", { input: formatTokens(quota.inputTokensToday), output: formatTokens(quota.outputTokensToday) })
                  : undefined
              }
              sparkline={sparklines?.tokenSparkline}
              trend={sparklines?.trends.tokens}
            />
            <StatCard
              icon={DollarSign}
              label={t("statCards.costToday", "Cost Today")}
              value={formatCost(quota?.costToday)}
              sparkline={sparklines?.costSparkline}
              trend={sparklines?.trends.cost}
            />
            <StatCard
              icon={Bot}
              label={t("statCards.agents")}
              value={
                agentTotal > 0
                  ? `${runningAgents} / ${agentTotal}`
                  : "0"
              }
              sub={agentTotal > 0 ? t("statCards.running") : undefined}
            />
            <StatCard
              icon={Radio}
              label={t("statCards.channels")}
              value={
                channelEntries.length > 0
                  ? `${channelsOnline} / ${channelEntries.length}`
                  : "0"
              }
              sub={channelEntries.length > 0 ? t("statCards.online") : undefined}
            />
          </div>

          {/* System Health */}
          <SystemHealthCard
            health={health}
            liveUptime={liveUptime}
            enabledProviderCount={enabledProviders.length}
            sessions={status?.sessions ?? 0}
            clientCount={clientList.length}
            channelEntries={channelEntries}
            runtimeEntries={runtimes?.runtimes}
          />

          {/* Connected Clients + Cron Jobs */}
          <div className="grid gap-4 lg:grid-cols-2">
            <ConnectedClientsCard
              clients={clientList}
              currentId={health?.currentId}
            />
            <CronJobsCard jobs={cronData?.jobs ?? []} />
          </div>

          {/* Recent Requests */}
          <RecentRequestsCard traces={traces} />

          {/* Quota Usage */}
          {quota?.enabled && quota.entries.length > 0 && (
            <QuotaUsageCard quota={quota} />
          )}
        </TabsContent>

        <TabsContent value="usage">
          <Suspense fallback={<div className="flex items-center justify-center py-12"><div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" /></div>}>
            <UsagePage />
          </Suspense>
        </TabsContent>
      </Tabs>
    </div>
  );
}
