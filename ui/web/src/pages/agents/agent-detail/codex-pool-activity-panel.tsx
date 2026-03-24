import { useMemo } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowUpRight, RefreshCw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { formatDuration, formatRelativeTime } from "@/lib/format";
import type { ChatGPTOAuthAvailability } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import type { CodexPoolRecentRequest } from "./hooks/use-codex-pool-activity";

export interface CodexPoolEntry {
  name: string;
  label: string;
  availability: ChatGPTOAuthAvailability;
  role: "preferred" | "extra";
  requestCount: number;
  lastUsedAt?: string;
  providerHref?: string;
}

interface CodexPoolActivityPanelProps {
  entries: CodexPoolEntry[];
  strategy: "manual" | "round_robin";
  recentRequests: CodexPoolRecentRequest[];
  loading: boolean;
  fetching: boolean;
  showProviderLinks?: boolean;
  onRefresh: () => void;
}

function statusBadgeClass(availability: ChatGPTOAuthAvailability): string {
  if (availability === "ready") return "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
  if (availability === "disabled") return "border-muted-foreground/30 bg-muted text-muted-foreground";
  return "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300";
}

export function CodexPoolActivityPanel({
  entries,
  strategy,
  recentRequests,
  loading,
  fetching,
  showProviderLinks = true,
  onRefresh,
}: CodexPoolActivityPanelProps) {
  const { t } = useTranslation("agents");
  const maxRequests = Math.max(1, ...entries.map((entry) => entry.requestCount));
  const observedProviders = entries.filter((entry) => entry.requestCount > 0).length;
  const totalProviders = entries.length;
  const switchCount = useMemo(
    () => recentRequests.slice(1).reduce(
      (count, request, index) => count + (request.provider_name !== recentRequests[index]?.provider_name ? 1 : 0),
      0,
    ),
    [recentRequests],
  );

  return (
    <Card className="overflow-hidden">
      <CardHeader className="border-b bg-muted/20">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <CardTitle>{t("chatgptOAuthRouting.activityTitle")}</CardTitle>
            <CardDescription>{t("chatgptOAuthRouting.activityDescription")}</CardDescription>
          </div>
          <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={onRefresh} disabled={fetching}>
            <RefreshCw className={`h-4 w-4${fetching ? " animate-spin" : ""}`} />
            {t("chatgptOAuthRouting.refreshEvidence")}
          </Button>
        </div>

        <div className="flex flex-wrap gap-2 pt-1">
          <Badge variant="outline">
            {strategy === "round_robin" ? t("chatgptOAuthRouting.strategy.roundRobin") : t("chatgptOAuthRouting.strategy.manual")}
          </Badge>
          <Badge variant="secondary">{t("chatgptOAuthRouting.recentRequestsCount", { count: recentRequests.length })}</Badge>
          <Badge variant="secondary">{t("chatgptOAuthRouting.observedProviders", { observed: observedProviders, total: totalProviders })}</Badge>
          {recentRequests.length > 1 && (
            <Badge variant="secondary">{t("chatgptOAuthRouting.switchRate", { switches: switchCount, total: recentRequests.length - 1 })}</Badge>
          )}
        </div>
      </CardHeader>

      <CardContent className="space-y-5 pt-5">
        <section className="space-y-3">
          <div>
            <h3 className="text-sm font-medium">{t("chatgptOAuthRouting.poolMembersTitle")}</h3>
            <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.poolMembersDescription")}</p>
          </div>
          <div className="space-y-3">
            {entries.map((entry) => (
              <div key={entry.name} className="space-y-2 rounded-lg border p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium">{entry.label}</span>
                      <Badge variant={entry.role === "preferred" ? "default" : "outline"}>{t(`chatgptOAuthRouting.role.${entry.role}`)}</Badge>
                      <Badge variant="outline" className={statusBadgeClass(entry.availability)}>
                        {t(`chatgptOAuthRouting.status.${entry.availability}`)}
                      </Badge>
                    </div>
                    <p className="text-xs font-mono text-muted-foreground">{entry.name}</p>
                  </div>
                  {showProviderLinks && entry.providerHref && (
                    <Button asChild variant="ghost" size="sm" className="h-7 px-0 text-xs shrink-0">
                      <Link to={entry.providerHref}>
                        {t("chatgptOAuthRouting.openProvider")}
                        <ArrowUpRight className="ml-1 h-3.5 w-3.5" />
                      </Link>
                    </Button>
                  )}
                </div>
                <div className="flex items-center gap-3 text-xs text-muted-foreground">
                  <span>{t("chatgptOAuthRouting.requestCount", { count: entry.requestCount })}</span>
                  <span>{t("chatgptOAuthRouting.lastUsedAt", {
                    value: entry.lastUsedAt ? formatRelativeTime(entry.lastUsedAt) : t("chatgptOAuthRouting.never"),
                  })}
                  </span>
                </div>
                <div className="h-2 rounded-full bg-muted">
                  <div
                    className="h-2 rounded-full bg-primary/70 transition-all"
                    style={{ width: `${Math.max((entry.requestCount / maxRequests) * 100, entry.requestCount > 0 ? 12 : 0)}%` }}
                  />
                </div>
              </div>
            ))}
          </div>
        </section>

        <section className="space-y-3">
          <div>
            <h3 className="text-sm font-medium">{t("chatgptOAuthRouting.sequenceTitle")}</h3>
            <p className="text-xs text-muted-foreground">{t("chatgptOAuthRouting.sequenceDescription")}</p>
          </div>
          {loading ? (
            <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.loadingEvidence")}
            </div>
          ) : recentRequests.length > 0 ? (
            <div className="grid gap-3 md:grid-cols-2">
              {recentRequests.map((request) => (
                <div key={request.trace_id} className="rounded-lg border p-3">
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <p className="text-sm font-medium">{request.provider_name}</p>
                      <p className="text-xs text-muted-foreground">{request.model || t("chatgptOAuthRouting.unknownModel")}</p>
                    </div>
                    <Badge variant={request.status === "completed" ? "outline" : "secondary"}>{request.status}</Badge>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted-foreground">
                    <span>{formatRelativeTime(request.started_at)}</span>
                    <span>{formatDuration(request.duration_ms)}</span>
                    <span>{t("chatgptOAuthRouting.poolLlmCalls", { count: request.pool_llm_calls })}</span>
                  </div>
                  {request.failover_providers && request.failover_providers.length > 0 && (
                    <p className="mt-3 text-xs text-amber-700 dark:text-amber-300">
                      {t("chatgptOAuthRouting.failoverHint", { providers: request.failover_providers.join(", ") })}
                    </p>
                  )}
                  <Button asChild variant="ghost" size="sm" className="mt-2 h-7 px-0 text-xs">
                    <Link to={`/traces/${request.trace_id}`}>
                      {t("chatgptOAuthRouting.openTrace")}
                      <ArrowUpRight className="ml-1 h-3.5 w-3.5" />
                    </Link>
                  </Button>
                </div>
              ))}
            </div>
          ) : (
            <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
              {t("chatgptOAuthRouting.noEvidence")}
            </div>
          )}
        </section>
      </CardContent>
    </Card>
  );
}
