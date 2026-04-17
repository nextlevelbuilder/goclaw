import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { FileText, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { formatDate } from "@/lib/format";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useRawMessages } from "./hooks/use-raw-messages";
import type { RawMessage } from "./hooks/use-raw-messages";
import { RawMessageDetailDialog } from "./raw-message-detail-dialog";

const PAGE_SIZE = 50;

export function RawMessagesPage() {
  const { t } = useTranslation("raw-messages");
  const { t: tc } = useTranslation("common");
  const { messages, total, loading, loadMessages } = useRawMessages();

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && messages.length === 0);
  const [offset, setOffset] = useState(0);
  const [filterProcessed, setFilterProcessed] = useState<"all" | "pending" | "processed">("all");
  const [selectedMsg, setSelectedMsg] = useState<RawMessage | null>(null);

  useEffect(() => {
    const params: { processed?: boolean; limit: number; offset: number } = {
      limit: PAGE_SIZE,
      offset,
    };
    if (filterProcessed === "pending") params.processed = false;
    if (filterProcessed === "processed") params.processed = true;
    loadMessages(params);
  }, [offset, filterProcessed, loadMessages]);

  const handleRefresh = () => {
    const params: { processed?: boolean; limit: number; offset: number } = {
      limit: PAGE_SIZE,
      offset,
    };
    if (filterProcessed === "pending") params.processed = false;
    if (filterProcessed === "processed") params.processed = true;
    loadMessages(params);
  };

  const totalPages = Math.ceil(total / PAGE_SIZE);
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex items-center gap-2">
            <select
              value={filterProcessed}
              onChange={(e) => {
                setFilterProcessed(e.target.value as "all" | "pending" | "processed");
                setOffset(0);
              }}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm"
            >
              <option value="all">{t("filters.all")}</option>
              <option value="pending">{t("filters.pending")}</option>
              <option value="processed">{t("filters.processed")}</option>
            </select>
            <Button variant="outline" size="sm" onClick={handleRefresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={6} />
        ) : messages.length === 0 ? (
          <EmptyState
            icon={FileText}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <>
            <div className="mb-2 text-xs text-muted-foreground">
              {t("showing", { count: messages.length, total })}
            </div>
            <div className="overflow-x-auto rounded-md border">
              <table className="w-full min-w-[600px] text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="px-4 py-3 text-left font-medium">{t("columns.chatName")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.sender")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.body")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.agent")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.graphId")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.timestamp")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  </tr>
                </thead>
                <tbody>
                  {messages.map((msg) => (
                    <tr key={msg.id} className="cursor-pointer border-b last:border-0 hover:bg-muted/30" onClick={() => setSelectedMsg(msg)}>
                      <td className="max-w-[180px] truncate px-4 py-3 font-medium">
                        {msg.chat_name || msg.chat_id}
                      </td>
                      <td className="max-w-[140px] truncate px-4 py-3">
                        {msg.sender}
                      </td>
                      <td className="max-w-[300px] truncate px-4 py-3 text-muted-foreground">
                        {msg.body}
                      </td>
                      <td className="max-w-[120px] truncate px-4 py-3">
                        {msg.agent_name || (msg.agent_id ? msg.agent_id.slice(0, 8) + "…" : "")}
                      </td>
                      <td className="max-w-[150px] truncate px-4 py-3 font-mono text-xs text-muted-foreground">
                        {msg.graph_id}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-muted-foreground">
                        {formatDate(msg.msg_timestamp || msg.created_at)}
                      </td>
                      <td className="px-4 py-3">
                        {msg.processed_at ? (
                          <Badge variant="success" className="text-xs">{t("status.processed")}</Badge>
                        ) : (
                          <Badge variant="secondary" className="text-xs">{t("status.pending")}</Badge>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {totalPages > 1 && (
              <div className="mt-3 flex items-center justify-between">
                <span className="text-xs text-muted-foreground">
                  {t("pagination.page", { current: currentPage, total: totalPages })}
                </span>
                <div className="flex gap-1">
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    disabled={offset === 0}
                    onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                  >
                    {tc("previous")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    disabled={offset + PAGE_SIZE >= total}
                    onClick={() => setOffset(offset + PAGE_SIZE)}
                  >
                    {tc("next")}
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </div>

      {selectedMsg && (
        <RawMessageDetailDialog message={selectedMsg} onClose={() => setSelectedMsg(null)} />
      )}
    </div>
  );
}
