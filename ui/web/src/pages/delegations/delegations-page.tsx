import { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRightLeft, RefreshCw, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
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
import { useWsEvent } from "@/hooks/use-ws-event";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { Events } from "@/api/protocol";
import { formatDate, formatDuration } from "@/lib/format";
import { useDelegations } from "./hooks/use-delegations";
import { useTraces } from "@/pages/traces/hooks/use-traces";
import { DelegationDetailDialog } from "./delegation-detail-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import type { AgentEventPayload } from "@/types/chat";
import type { DelegationHistoryRecord } from "@/types/delegation";

export function DelegationsPage() {
  const { t } = useTranslation("delegations");
  const { t: tc } = useTranslation("common");
  const { delegations, total, loading, load, getDelegation } = useDelegations();
  const { getTrace } = useTraces();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && delegations.length === 0);
  const [sourceFilter, setSourceFilter] = useState("");
  const [targetFilter, setTargetFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  const sourceRef = useRef(sourceFilter);
  sourceRef.current = sourceFilter;
  const targetRef = useRef(targetFilter);
  targetRef.current = targetFilter;
  const statusRef = useRef(statusFilter);
  statusRef.current = statusFilter;
  const pageRef = useRef(page);
  pageRef.current = page;
  const pageSizeRef = useRef(pageSize);
  pageSizeRef.current = pageSize;

  const buildFilters = useCallback(() => ({
    source_agent_id: sourceRef.current || undefined,
    target_agent_id: targetRef.current || undefined,
    status: statusRef.current !== "all" ? statusRef.current : undefined,
    limit: pageSizeRef.current,
    offset: (pageRef.current - 1) * pageSizeRef.current,
  }), []);

  useEffect(() => {
    load({ limit: pageSize, offset: (page - 1) * pageSize });
  }, [load, page, pageSize]);

  const handleRefresh = () => load(buildFilters());

  const debouncedRefresh = useDebouncedCallback(() => load(buildFilters()), 3000);

  const handleAgentEvent = useCallback(
    (payload: unknown) => {
      const event = payload as AgentEventPayload;
      if (!event) return;
      if (event.type === "run.completed" || event.type === "run.failed") {
        debouncedRefresh();
      }
    },
    [debouncedRefresh],
  );

  useWsEvent(Events.AGENT, handleAgentEvent);

  const handleFilterSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setPage(1);
    handleRefresh();
  };

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <Button variant="outline" size="sm" onClick={handleRefresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
          </Button>
        }
      />

      <form onSubmit={handleFilterSubmit} className="mt-4 flex flex-wrap gap-2">
        <div className="relative max-w-[200px] flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={sourceFilter}
            onChange={(e) => setSourceFilter(e.target.value)}
            placeholder={t("sourceFilterPlaceholder")}
            className="pl-9"
          />
        </div>
        <div className="relative max-w-[200px] flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={targetFilter}
            onChange={(e) => setTargetFilter(e.target.value)}
            placeholder={t("targetFilterPlaceholder")}
            className="pl-9"
          />
        </div>
        <Select value={statusFilter} onValueChange={setStatusFilter}>
          <SelectTrigger className="w-[140px]">
            <SelectValue placeholder="Status" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("statusFilter.all")}</SelectItem>
            <SelectItem value="pending">{t("statusFilter.pending")}</SelectItem>
            <SelectItem value="running">{t("statusFilter.running")}</SelectItem>
            <SelectItem value="completed">{t("statusFilter.completed")}</SelectItem>
            <SelectItem value="failed">{t("statusFilter.failed")}</SelectItem>
            <SelectItem value="cancelled">{t("statusFilter.cancelled")}</SelectItem>
          </SelectContent>
        </Select>
        <Button type="submit" variant="outline" size="sm">
          {t("filter")}
        </Button>
      </form>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={8} />
        ) : delegations.length === 0 ? (
          <EmptyState
            icon={ArrowRightLeft}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("columns.sourceTarget")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.task")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.mode")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.duration")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.time")}</th>
                </tr>
              </thead>
              <tbody>
                {delegations.map((d: DelegationHistoryRecord) => (
                  <tr
                    key={d.id}
                    className="cursor-pointer border-b last:border-0 hover:bg-muted/30"
                    onClick={() => setSelectedId(d.id)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-medium">{d.source_agent_key || d.source_agent_id.slice(0, 8)}</span>
                      <span className="mx-1 text-muted-foreground">&rarr;</span>
                      <span className="font-medium">{d.target_agent_key || d.target_agent_id.slice(0, 8)}</span>
                    </td>
                    <td className="max-w-[300px] truncate px-4 py-3 text-muted-foreground">
                      {d.task}
                    </td>
                    <td className="px-4 py-3">
                      <StatusBadge status={d.status} />
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="outline" className="text-xs">{d.mode}</Badge>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {formatDuration(d.duration_ms)}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {formatDate(d.created_at)}
                    </td>
                  </tr>
                ))}
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

      {selectedId && (
        <DelegationDetailDialog
          delegationId={selectedId}
          onClose={() => setSelectedId(null)}
          getDelegation={getDelegation}
          getTrace={getTrace}
        />
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const variant =
    status === "completed"
      ? "success"
      : status === "failed"
        ? "destructive"
        : status === "running" || status === "pending"
          ? "info"
          : "secondary";

  return <Badge variant={variant} className="text-xs">{status || "unknown"}</Badge>;
}
