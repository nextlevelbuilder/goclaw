import { useState, useEffect } from "react";
import { ClipboardList, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
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
import { formatDate } from "@/lib/format";
import { useActivity } from "./hooks/use-activity";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";

const ACTION_COLORS: Record<string, string> = {
  "agent.created": "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300",
  "agent.updated": "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300",
  "agent.deleted": "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-300",
};

export function ActivityPage() {
  const { t } = useTranslation("activity");
  const { logs, total, loading, load } = useActivity();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && logs.length === 0);
  const [actionFilter, setActionFilter] = useState("all");
  const [entityFilter, setEntityFilter] = useState("all");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  useEffect(() => {
    load({
      action: actionFilter !== "all" ? actionFilter : undefined,
      entity_type: entityFilter !== "all" ? entityFilter : undefined,
      limit: pageSize,
      offset: (page - 1) * pageSize,
    });
  }, [page, pageSize, actionFilter, entityFilter]);

  const handleRefresh = () => {
    load({
      action: actionFilter !== "all" ? actionFilter : undefined,
      entity_type: entityFilter !== "all" ? entityFilter : undefined,
      limit: pageSize,
      offset: (page - 1) * pageSize,
    });
  };

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <Button variant="outline" size="sm" onClick={handleRefresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {t("common:refresh", "Refresh")}
          </Button>
        }
      />

      {/* Filters */}
      <div className="mb-4 flex flex-wrap gap-2">
        <Select value={actionFilter} onValueChange={(v) => { setActionFilter(v); setPage(1); }}>
          <SelectTrigger className="w-[180px]">
            <SelectValue placeholder={t("filters.action")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("filters.allActions")}</SelectItem>
            <SelectItem value="agent.created">{t("filters.agentCreated")}</SelectItem>
            <SelectItem value="agent.updated">{t("filters.agentUpdated")}</SelectItem>
            <SelectItem value="agent.deleted">{t("filters.agentDeleted")}</SelectItem>
          </SelectContent>
        </Select>

        <Select value={entityFilter} onValueChange={(v) => { setEntityFilter(v); setPage(1); }}>
          <SelectTrigger className="w-[180px]">
            <SelectValue placeholder={t("filters.entityType")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("filters.allEntities")}</SelectItem>
            <SelectItem value="agent">{t("filters.agent")}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {showSkeleton ? (
        <TableSkeleton rows={8} />
      ) : logs.length === 0 ? (
        <EmptyState
          icon={ClipboardList}
          title={t("empty.title")}
          description={t("empty.description")}
        />
      ) : (
        <>
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-3 py-2 text-left font-medium">{t("table.action")}</th>
                  <th className="px-3 py-2 text-left font-medium">{t("table.actor")}</th>
                  <th className="px-3 py-2 text-left font-medium">{t("table.entity")}</th>
                  <th className="px-3 py-2 text-left font-medium">{t("table.entityId")}</th>
                  <th className="px-3 py-2 text-left font-medium">{t("table.ip")}</th>
                  <th className="px-3 py-2 text-left font-medium">{t("table.time")}</th>
                </tr>
              </thead>
              <tbody>
                {logs.map((log) => (
                  <tr key={log.id} className="border-b hover:bg-muted/30">
                    <td className="px-3 py-2">
                      <Badge variant="secondary" className={ACTION_COLORS[log.action] ?? ""}>
                        {log.action}
                      </Badge>
                    </td>
                    <td className="px-3 py-2 font-mono text-xs">
                      {log.actor_type}:{log.actor_id}
                    </td>
                    <td className="px-3 py-2">{log.entity_type || "—"}</td>
                    <td className="px-3 py-2 font-mono text-xs max-w-[200px] truncate">
                      {log.entity_id || "—"}
                    </td>
                    <td className="px-3 py-2 text-muted-foreground text-xs">
                      {log.ip_address || "—"}
                    </td>
                    <td className="px-3 py-2 text-muted-foreground text-xs whitespace-nowrap">
                      {formatDate(log.created_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="mt-4">
            <Pagination
              page={page}
              totalPages={totalPages}
              pageSize={pageSize}
              total={total}
              onPageChange={setPage}
              onPageSizeChange={(s) => { setPageSize(s); setPage(1); }}
            />
          </div>
        </>
      )}
    </div>
  );
}
