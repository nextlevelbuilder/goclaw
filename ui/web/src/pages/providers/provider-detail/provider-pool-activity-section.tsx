import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { Activity, ExternalLink, RefreshCw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/shared/empty-state";
import { cn } from "@/lib/utils";
import type { CodexPoolProviderCount } from "@/pages/agents/agent-detail/hooks/use-codex-pool-activity";
import { CodexPoolMemberCard } from "@/pages/agents/agent-detail/codex-pool-member-card";
import { CodexPoolRecentRequestsList } from "@/pages/agents/agent-detail/codex-pool-recent-requests-list";
import type { CodexPoolEntry } from "@/pages/agents/agent-detail/codex-pool-entry-types";
import type { ChatGPTOAuthAvailability } from "../hooks/use-chatgpt-oauth-provider-statuses";
import type { ChatGPTOAuthProviderQuota } from "../hooks/use-chatgpt-oauth-provider-quotas";
import type { ProviderData } from "../hooks/use-providers";
import type { ProviderCodexPoolAgentCount } from "../hooks/use-provider-codex-pool-activity";
import type { CodexPoolRecentRequest } from "@/pages/agents/agent-detail/hooks/use-codex-pool-activity";

function MonitorStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border bg-background/70 px-2 py-1">
      <p className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground xl:text-[10px]">
        {label}
      </p>
      <p className="mt-0.5 text-[13px] font-semibold leading-tight tabular-nums xl:text-sm">
        {value}
      </p>
    </div>
  );
}

interface ProviderPoolActivitySectionProps {
  provider: ProviderData;
  providerCounts: CodexPoolProviderCount[];
  recentRequests: CodexPoolRecentRequest[];
  topAgents: ProviderCodexPoolAgentCount[];
  statsSampleSize: number;
  fetching: boolean;
  onRefresh: () => void;
  providerByName: Map<string, ProviderData>;
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>;
  quotaByName: Map<string, ChatGPTOAuthProviderQuota | null>;
}

export function ProviderPoolActivitySection({
  provider,
  providerCounts,
  recentRequests,
  topAgents,
  statsSampleSize,
  fetching,
  onRefresh,
  providerByName,
  statusByName,
  quotaByName,
}: ProviderPoolActivitySectionProps) {
  const { t } = useTranslation("providers");
  const { t: ta } = useTranslation("agents");

  const entries = useMemo<CodexPoolEntry[]>(() => {
    return providerCounts.map((c) => {
      const p = providerByName.get(c.provider_name);
      const availability: ChatGPTOAuthAvailability =
        statusByName.get(c.provider_name)?.availability ??
        (p?.enabled === false ? "disabled" : "needs_sign_in");
      return {
        name: c.provider_name,
        label: p?.display_name || c.provider_name,
        availability,
        role: c.provider_name === provider.name ? "preferred" as const : "extra" as const,
        requestCount: c.request_count,
        directSelectionCount: c.direct_selection_count,
        failoverServeCount: c.failover_serve_count,
        successCount: c.success_count,
        failureCount: c.failure_count,
        consecutiveFailures: c.consecutive_failures,
        successRate: c.success_rate,
        healthScore: c.health_score,
        healthState: c.health_state ?? "idle",
        lastSelectedAt: c.last_selected_at,
        lastFailoverAt: c.last_failover_at,
        lastUsedAt: c.last_used_at,
        lastSuccessAt: c.last_success_at,
        lastFailureAt: c.last_failure_at,
        providerHref: p?.id ? `/providers/${p.id}` : undefined,
        quota: quotaByName.get(c.provider_name),
      };
    });
  }, [provider.name, providerByName, providerCounts, quotaByName, statusByName]);

  const failoverCount = recentRequests.filter((r) => r.used_failover).length;

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium">{t("detail.poolActivityTitle")}</h3>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 gap-1.5 px-2"
          onClick={onRefresh}
          disabled={fetching}
          aria-busy={fetching}
        >
          <RefreshCw className={cn("h-3.5 w-3.5", fetching && "animate-spin")} />
          {t("detail.poolActivityRefresh")}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        {t("detail.poolActivityDescription")}
      </p>

      {/* Summary stats */}
      <div className="grid gap-1.5 grid-cols-2 sm:grid-cols-4">
        <MonitorStat
          label={ta("chatgptOAuthRouting.metrics.poolSize")}
          value={String(entries.length)}
        />
        <MonitorStat
          label={ta("chatgptOAuthRouting.metrics.observedSample")}
          value={String(statsSampleSize)}
        />
        <MonitorStat
          label={t("detail.poolActivityTopAgents")}
          value={String(topAgents.length)}
        />
        <MonitorStat
          label={ta("chatgptOAuthRouting.metrics.failovers")}
          value={String(failoverCount)}
        />
      </div>

      {/* Pool member cards */}
      {entries.length > 0 ? (
        <div className="grid auto-rows-min gap-2 [grid-template-columns:repeat(auto-fit,minmax(min(100%,12.25rem),1fr))]">
          {entries.map((entry) => (
            <CodexPoolMemberCard key={entry.name} entry={entry} showProviderLinks />
          ))}
        </div>
      ) : (
        <EmptyState
          icon={Activity}
          title={t("detail.poolActivityEmpty")}
          description={t("detail.poolActivityEmptyDesc")}
          className="py-6"
        />
      )}

      {/* Recent requests */}
      {recentRequests.length > 0 ? (
        <div className="space-y-1.5">
          <div className="flex items-center justify-between gap-2">
            <h4 className="text-xs font-medium text-muted-foreground">
              {ta("chatgptOAuthRouting.sequenceTitle")}
            </h4>
            <Badge variant="outline" className="text-[10px]">
              {ta("chatgptOAuthRouting.recentRequestsCount", {
                count: recentRequests.length,
              })}
            </Badge>
          </div>
          <CodexPoolRecentRequestsList
            recentRequests={recentRequests}
            loading={fetching && recentRequests.length === 0}
            compact
          />
        </div>
      ) : null}

      {/* Top agents */}
      {topAgents.length > 0 ? (
        <div className="space-y-1.5">
          <h4 className="text-xs font-medium text-muted-foreground">
            {t("detail.poolActivityTopAgentsTitle")}
          </h4>
          <ul className="space-y-1 list-none p-0 m-0" role="list">
            {topAgents.map((agent) => (
              <li
                key={agent.agent_id}
                className="flex items-center justify-between rounded-md border bg-muted/30 px-2.5 py-1.5 text-xs"
              >
                <div className="flex items-center gap-1.5">
                  {agent.agent_key ? (
                    <Link
                      to={`/agents/${agent.agent_id}/codex-pool`}
                      className="font-medium text-foreground hover:underline"
                    >
                      {agent.agent_key}
                    </Link>
                  ) : (
                    <span className="font-mono text-muted-foreground">
                      {agent.agent_id.slice(0, 8)}
                    </span>
                  )}
                  <Link
                    to={`/agents/${agent.agent_id}/codex-pool`}
                    className="text-muted-foreground hover:text-foreground"
                    aria-label={`View pool details for ${agent.agent_key || agent.agent_id}`}
                  >
                    <ExternalLink className="h-3 w-3" />
                  </Link>
                </div>
                <span className="tabular-nums text-muted-foreground">
                  {agent.request_count} {t("detail.poolActivityRequests")}
                </span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </section>
  );
}
