import { useEffect, useCallback } from "react";
import { Activity, Bot, Hash, Radio, AlertTriangle } from "lucide-react";
import { Link } from "react-router";
import { PageHeader } from "@/components/shared/page-header";
import { StatusBadge } from "@/components/shared/status-badge";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { useAuthStore } from "@/stores/use-auth-store";
import { useWsCall } from "@/hooks/use-ws-call";
import { useWsEvent } from "@/hooks/use-ws-event";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useTraces } from "@/pages/traces/hooks/use-traces";
import { Methods, Events } from "@/api/protocol";
import { ROUTES } from "@/lib/constants";
import { formatTokens } from "@/lib/format";

import type {
  HealthPayload,
  StatusPayload,
  QuotaUsageResult,
  CronListPayload,
  ChannelStatusPayload,
} from "./types";
import { useLiveUptime } from "./hooks/use-live-uptime";
import { StatCard } from "./stat-card";
import { SystemHealthCard } from "./system-health-card";
import { ConnectedClientsCard } from "./connected-clients-card";
import { CronJobsCard } from "./cron-jobs-card";
import { RecentRequestsCard } from "./recent-requests-card";
import { QuotaUsageCard } from "./quota-usage-card";

const REFRESH_INTERVAL = 30_000;

export function OverviewPage() {
  const connected = useAuthStore((s) => s.connected);
  const { call: fetchHealth, data: health } =
    useWsCall<HealthPayload>(Methods.HEALTH);
  const { call: fetchStatus, data: status } =
    useWsCall<StatusPayload>(Methods.STATUS);
  const { call: fetchQuota, data: quota } =
    useWsCall<QuotaUsageResult>(Methods.QUOTA_USAGE);
  const { call: fetchCron, data: cronData } =
    useWsCall<CronListPayload>(Methods.CRON_LIST);
  const { call: fetchChannels, data: channelStatusData } =
    useWsCall<ChannelStatusPayload>(Methods.CHANNELS_STATUS);
  const { providers, loading: providersLoading } = useProviders();
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
        title="Dashboard"
        description="Gateway overview and quota usage"
        actions={
          <div className="flex items-center gap-2">
            {health?.version && (
              <span className="text-xs text-muted-foreground">
                v{health.version}
              </span>
            )}
            <StatusBadge
              status={connected ? "success" : "error"}
              label={connected ? "Connected" : "Disconnected"}
            />
          </div>
        }
      />

      {/* Provider warning */}
      {(hasNoProviders || hasNoEnabledProviders) && (
        <Alert>
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>
            {hasNoProviders
              ? "No LLM providers configured"
              : "No LLM providers enabled"}
          </AlertTitle>
          <AlertDescription>
            {hasNoProviders
              ? "You need to add at least one LLM provider before agents can work. "
              : "All providers are currently disabled. Enable at least one to start using agents. "}
            <Link
              to={ROUTES.PROVIDERS}
              className="font-medium underline underline-offset-4 hover:text-foreground"
            >
              Go to Provider Settings
            </Link>
          </AlertDescription>
        </Alert>
      )}

      {/* Summary cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          icon={Activity}
          label="Requests Today"
          value={quota?.requestsToday ?? 0}
          sub={
            quota?.uniqueUsersToday
              ? `${quota.uniqueUsersToday} users`
              : undefined
          }
        />
        <StatCard
          icon={Hash}
          label="Tokens Today"
          value={formatTokens(
            (quota?.inputTokensToday ?? 0) + (quota?.outputTokensToday ?? 0),
          )}
          sub={
            quota
              ? `${formatTokens(quota.inputTokensToday)} in / ${formatTokens(quota.outputTokensToday)} out`
              : undefined
          }
        />
        <StatCard
          icon={Bot}
          label="Agents"
          value={
            agentTotal > 0
              ? `${runningAgents} / ${agentTotal}`
              : "0"
          }
          sub={agentTotal > 0 ? "running" : undefined}
        />
        <StatCard
          icon={Radio}
          label="Channels"
          value={
            channelEntries.length > 0
              ? `${channelsOnline} / ${channelEntries.length}`
              : "0"
          }
          sub={channelEntries.length > 0 ? "online" : undefined}
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
    </div>
  );
}
