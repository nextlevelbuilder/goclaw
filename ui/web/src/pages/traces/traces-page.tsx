import { useState, useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Activity, GitFork, RefreshCw, Square, Bot, User, Users, Clock, Network, Globe, CheckCircle2, XCircle, Loader2, CircleDot, CircleDashed } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { formatDate, formatDuration, formatTokens, computeDurationMs } from "@/lib/format";
import { formatUserLabel } from "@/lib/format-user-label";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { useTraces, type TraceData } from "./hooks/use-traces";
import { TraceDetailDialog } from "./trace-detail-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useUiStore } from "@/stores/use-ui-store";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";
import { useQueryClient } from "@tanstack/react-query";
import { useWs } from "@/hooks/use-ws";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Methods, Events } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";

/** Strip media placeholder tags like <media:image> from preview text */
function cleanPreview(text: string): string {
  if (!text) return text;
  return text.replace(/<media:\w+>/g, "[media]");
}

/** Parse session_key to extract source type: Direct, Group, Cron, Team, WS */
function parseSourceType(sessionKey: string): { type: string; topic?: string } {
  if (!sessionKey) return { type: "unknown" };
  if (sessionKey.includes(":cron:")) return { type: "cron" };
  if (sessionKey.includes(":team:")) return { type: "team" };
  const topicMatch = sessionKey.match(/:topic:(\d+)/);
  if (topicMatch) return { type: "group", topic: topicMatch[1] };
  if (sessionKey.includes(":group:")) return { type: "group" };
  if (sessionKey.includes(":ws:")) return { type: "ws" };
  if (sessionKey.includes(":direct:")) return { type: "direct" };
  return { type: "unknown" };
}

const SOURCE_ICONS: Record<string, typeof Bot> = {
  cron: Clock,
  team: Network,
  group: Users,
  direct: User,
  ws: Globe,
};

export function TracesPage() {
  const { t } = useTranslation("traces");
  const { t: tc } = useTranslation("common");
  const tz = useUiStore((s) => s.timezone);
  const globalPageSize = useUiStore((s) => s.pageSize);
  const setGlobalPageSize = useUiStore((s) => s.setPageSize);
  const [agentFilter, setAgentFilter] = useState<string>();
  const [channelFilter, setChannelFilter] = useState<string>();
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSizeRaw] = useState(globalPageSize);
  const setPageSize = (size: number) => { setPageSizeRaw(size); setPage(1); setGlobalPageSize(size); };

  const ws = useWs();
  const queryClient = useQueryClient();

  // Invalidate traces list on immediate status events (no need to wait for 5s flush).
  useWsEvent(Events.TRACE_STATUS, useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.traces.all }),
    [queryClient],
  ));

  const { agents } = useAgents();
  const { instances: channels } = useChannelInstances();

  const agentMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) map.set(a.id, a.display_name || a.agent_key || a.id);
    return map;
  }, [agents]);

  const [abortingRunId, setAbortingRunId] = useState<string | null>(null);

  const { traces, total, loading, fetching, refresh, getTrace } = useTraces({
    agentId: agentFilter,
    channel: channelFilter,
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });

  const traceUserIds = useMemo(() => traces.map((tr) => tr.user_id).filter(Boolean) as string[], [traces]);
  const { resolve } = useContactResolver(traceUserIds);

  const spinning = useMinLoading(fetching);
  const showSkeleton = useDeferredLoading(loading && traces.length === 0);

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  const handleAbortRun = useCallback(
    async (trace: TraceData, e: React.MouseEvent) => {
      e.stopPropagation();
      if (!ws.isConnected || abortingRunId) return;
      setAbortingRunId(trace.run_id);
      try {
        const res = await ws.call(Methods.CHAT_ABORT, {
          sessionKey: trace.session_key,
          runId: trace.run_id,
        }) as {
          aborted?: boolean;
          stopped?: boolean;
          forced?: boolean;
          alreadyAborting?: boolean;
          notFound?: boolean;
          unauthorized?: boolean;
        };
        if (res?.stopped) {
          toast.success(t("toast.abortStopped"));
        } else if (res?.forced) {
          toast.warning(t("toast.abortForced"));
        } else if (res?.alreadyAborting) {
          toast.info(t("toast.abortAlreadyAborting"));
        } else if (res?.unauthorized) {
          toast.error(t("toast.abortUnauthorized"));
        } else if (res?.notFound) {
          toast.info(t("toast.abortNotFound"));
        } else {
          toast.error(t("toast.abortFailed"));
        }
        refresh();
      } catch {
        toast.error(t("toast.abortFailed"));
      } finally {
        // Auto re-enable within 5s max (3s grace + 2s buffer) in case WS event is delayed.
        setTimeout(() => setAbortingRunId(null), 5000);
        setAbortingRunId(null);
      }
    },
    [ws, t, refresh, abortingRunId],
  );

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
          </Button>
        }
      />

      <div className="mt-4 flex flex-wrap items-center gap-2">
        {/* Agent filter */}
        <Select
          value={agentFilter ?? "__all__"}
          onValueChange={(v) => { setAgentFilter(v === "__all__" ? undefined : v); setPage(1); }}
        >
          <SelectTrigger className="h-8 w-44 text-xs">
            <SelectValue placeholder={t("allAgents")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{t("allAgents")}</SelectItem>
            {agents.map((a) => (
              <SelectItem key={a.id} value={a.id}>{a.display_name || a.agent_key || a.id}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Channel filter */}
        <Select
          value={channelFilter ?? "__all__"}
          onValueChange={(v) => { setChannelFilter(v === "__all__" ? undefined : v); setPage(1); }}
        >
          <SelectTrigger className="h-8 w-44 text-xs">
            <SelectValue placeholder={t("allChannels")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{t("allChannels")}</SelectItem>
            {channels.map((ch) => (
              <SelectItem key={ch.id} value={ch.name}>{ch.display_name || ch.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={8} />
        ) : traces.length === 0 ? (
          <EmptyState
            icon={Activity}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium max-w-[40%]">{t("columns.name")}</th>
                  <th className="px-3 py-3 text-center font-medium w-10"></th>
                  <th className="px-4 py-3 text-left font-medium whitespace-nowrap">{t("columns.tokens")}</th>
                  <th className="px-4 py-3 text-center font-medium whitespace-nowrap">{t("columns.spans")}</th>
                  <th className="px-4 py-3 text-right font-medium whitespace-nowrap">{t("columns.time")}</th>
                </tr>
              </thead>
              <tbody>
                {traces.map((trace: TraceData) => {
                  const source = parseSourceType(trace.session_key);
                  const userLabel = formatUserLabel(trace.user_id, resolve);
                  const agentName = trace.agent_id ? agentMap.get(trace.agent_id) : undefined;
                  const SourceIcon = SOURCE_ICONS[source.type] || Bot;

                  return (
                    <tr
                      key={trace.id}
                      className="cursor-pointer border-b last:border-0 hover:bg-muted/30"
                      onClick={() => setSelectedTraceId(trace.id)}
                    >
                      <td className="px-4 py-2.5 max-w-[300px] lg:max-w-[400px]">
                        <div className="flex items-center gap-1.5 text-sm font-medium min-w-0">
                          {trace.parent_trace_id && (
                            <GitFork className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                          )}
                          <span className="truncate">{agentName || trace.name || t("unnamed")}</span>
                          {userLabel && (
                            <>
                              <span className="shrink-0 text-muted-foreground">·</span>
                              <span className="truncate text-xs text-muted-foreground max-w-[140px]">{userLabel}</span>
                            </>
                          )}
                        </div>
                        <div className="mt-0.5 flex items-center gap-1">
                          <Badge variant="outline" className="shrink-0 gap-0.5 text-2xs px-1.5 py-0">
                            <SourceIcon className="h-2.5 w-2.5" />
                            {t(`source.${source.type}`)}
                            {source.topic && ` #${source.topic}`}
                          </Badge>
                          {trace.channel && (
                            <Badge variant="secondary" className="shrink-0 text-2xs px-1.5 py-0">
                              {trace.channel}
                            </Badge>
                          )}
                          {trace.input_preview && (
                            <span className="truncate text-xs text-muted-foreground">
                              {cleanPreview(trace.input_preview)}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-3 py-2.5 text-center">
                        <div className="flex items-center justify-center gap-1">
                          <StatusIcon status={trace.status} />
                          {(trace.status === "running") && (
                            <Button
                              variant="destructive"
                              size="icon-xs"
                              onClick={(e) => handleAbortRun(trace, e)}
                              disabled={abortingRunId === trace.run_id}
                              title={t("stopRun")}
                            >
                              <Square className="h-3 w-3" />
                            </Button>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground whitespace-nowrap">
                        <div>{formatTokens(trace.total_input_tokens)} / {formatTokens(trace.total_output_tokens)}</div>
                        {(trace.metadata?.total_cache_read_tokens ?? 0) > 0 && (
                          <div className="text-xs text-green-400">
                            {formatTokens(trace.metadata!.total_cache_read_tokens!)} {t("cached")}
                          </div>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-center text-muted-foreground">
                        {trace.span_count}
                      </td>
                      <td className="px-4 py-2.5 text-right text-muted-foreground whitespace-nowrap">
                        <div>{formatDate(trace.start_time, tz)}</div>
                        <div className="text-xs">{formatDuration(trace.duration_ms || computeDurationMs(trace.start_time, trace.end_time))}</div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <Pagination
              page={page}
              pageSize={pageSize}
              total={total}
              totalPages={totalPages}
              onPageChange={setPage}
              onPageSizeChange={(size) => { setPageSize(size); setPage(1); }}
            />
          </div>
        )}
      </div>

      {selectedTraceId && (
        <TraceDetailDialog
          traceId={selectedTraceId}
          onClose={() => setSelectedTraceId(null)}
          getTrace={getTrace}
          onNavigateTrace={setSelectedTraceId}
          onAbortRun={handleAbortRun}
        />
      )}
    </div>
  );
}

function StatusIcon({ status }: { status: string }) {
  if (status === "ok" || status === "success" || status === "completed") {
    return <CheckCircle2 className="h-4 w-4 text-green-500" />;
  }
  if (status === "error" || status === "failed") {
    return <XCircle className="h-4 w-4 text-destructive" />;
  }
  if (status === "running") {
    return <Loader2 className="h-4 w-4 text-blue-500 animate-spin" />;
  }
  if (status === "pending") {
    return <CircleDashed className="h-4 w-4 text-muted-foreground" />;
  }
  return <CircleDot className="h-4 w-4 text-muted-foreground" />;
}
