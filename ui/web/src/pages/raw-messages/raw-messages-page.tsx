import { useState, useEffect, useMemo, useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { FileText, RefreshCw, X, Filter, RotateCcw } from "lucide-react";
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
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { formatDate } from "@/lib/format";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useRawMessages } from "./hooks/use-raw-messages";
import type { RawMessage } from "./hooks/use-raw-messages";
import { RawMessageDetailDialog } from "./raw-message-detail-dialog";

const PAGE_SIZE = 50;

export function RawMessagesPage() {
  const { t } = useTranslation("raw-messages");
  const { t: tc } = useTranslation("common");
  const { agents } = useAgents();
  const { messages, total, loading, loadMessages, resetToPending } = useRawMessages();

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && messages.length === 0);
  const [offset, setOffset] = useState(0);
  const [showFilters, setShowFilters] = useState(false);
  const [selectedMsg, setSelectedMsg] = useState<RawMessage | null>(null);

  // Server-side filters
  const [filterProcessed, setFilterProcessed] = useState<"all" | "pending" | "processed">("all");
  const [filterAgentId, setFilterAgentId] = useState<string>("__all__");
  const [filterChannel, setFilterChannel] = useState("");
  const [filterGraphId, setFilterGraphId] = useState("");

  // Client-side text search
  const [searchChat, setSearchChat] = useState("");
  const [searchSender, setSearchSender] = useState("");
  const [searchBody, setSearchBody] = useState("");

  // Row selection
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  // Debounced text inputs for server-side filters
  const channelTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const graphTimer = useRef<ReturnType<typeof setTimeout>>(undefined);

  const handleChannelChange = useCallback((v: string) => {
    setFilterChannel(v);
    clearTimeout(channelTimer.current);
    channelTimer.current = setTimeout(() => setOffset(0), 400);
  }, []);

  const handleGraphIdChange = useCallback((v: string) => {
    setFilterGraphId(v);
    clearTimeout(graphTimer.current);
    graphTimer.current = setTimeout(() => setOffset(0), 400);
  }, []);

  // Reset offset when server-side filters change
  useEffect(() => { setOffset(0); setSelectedIds(new Set()); }, [filterProcessed, filterAgentId]);

  // Fetch data
  useEffect(() => {
    const params: {
      processed?: boolean;
      limit: number;
      offset: number;
      channelName?: string;
      agentId?: string;
      graphId?: string;
    } = { limit: PAGE_SIZE, offset };
    if (filterProcessed === "pending") params.processed = false;
    if (filterProcessed === "processed") params.processed = true;
    if (filterChannel) params.channelName = filterChannel;
    if (filterAgentId !== "__all__") params.agentId = filterAgentId;
    if (filterGraphId) params.graphId = filterGraphId;
    loadMessages(params);
  }, [offset, filterProcessed, filterAgentId, filterChannel, filterGraphId, loadMessages]);

  // Client-side filtered messages
  const filtered = useMemo(() => {
    let result = messages;
    if (searchChat) {
      const q = searchChat.toLowerCase();
      result = result.filter(
        (m) => m.chat_name.toLowerCase().includes(q) || m.chat_id.toLowerCase().includes(q),
      );
    }
    if (searchSender) {
      const q = searchSender.toLowerCase();
      result = result.filter(
        (m) => m.sender.toLowerCase().includes(q) || m.sender_id.toLowerCase().includes(q),
      );
    }
    if (searchBody) {
      const q = searchBody.toLowerCase();
      result = result.filter((m) => m.body.toLowerCase().includes(q));
    }
    return result;
  }, [messages, searchChat, searchSender, searchBody]);

  // Active filter count
  const activeFilterCount = useMemo(() => {
    let count = 0;
    if (filterProcessed !== "all") count++;
    if (filterAgentId !== "__all__") count++;
    if (filterChannel) count++;
    if (filterGraphId) count++;
    return count;
  }, [filterProcessed, filterAgentId, filterChannel, filterGraphId]);

  // Selection helpers
  const toggleSelect = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleSelectAll = useCallback(() => {
    setSelectedIds((prev) => {
      if (prev.size === filtered.length) return new Set();
      return new Set(filtered.map((m) => m.id));
    });
  }, [filtered]);

  // Clear selection when data changes
  useEffect(() => {
    setSelectedIds((prev) => {
      const ids = new Set<string>();
      for (const id of prev) {
        if (filtered.some((m) => m.id === id)) ids.add(id);
      }
      return ids.size === prev.size ? prev : ids;
    });
  }, [filtered]);

  const handleClearFilters = () => {
    setFilterProcessed("all");
    setFilterAgentId("__all__");
    setFilterChannel("");
    setFilterGraphId("");
    setSearchChat("");
    setSearchSender("");
    setSearchBody("");
    setOffset(0);
  };

  const handleRefresh = () => {
    const params: {
      processed?: boolean;
      limit: number;
      offset: number;
      channelName?: string;
      agentId?: string;
      graphId?: string;
    } = { limit: PAGE_SIZE, offset };
    if (filterProcessed === "pending") params.processed = false;
    if (filterProcessed === "processed") params.processed = true;
    if (filterChannel) params.channelName = filterChannel;
    if (filterAgentId !== "__all__") params.agentId = filterAgentId;
    if (filterGraphId) params.graphId = filterGraphId;
    loadMessages(params);
  };

  const handleResetToPending = async () => {
    if (selectedIds.size === 0) return;
    try {
      const count = await resetToPending([...selectedIds]);
      setSelectedIds(new Set());
      handleRefresh();
      // Brief toast via badge update
      alert(t("actions.resetSuccess", { count }));
    } catch {
      // ignore
    }
  };

  const handleResetSingle = async (msg: RawMessage) => {
    try {
      await resetToPending([msg.id]);
      setSelectedMsg(null);
      handleRefresh();
    } catch {
      // ignore
    }
  };

  const totalPages = Math.ceil(total / PAGE_SIZE);
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;
  const allSelected = filtered.length > 0 && selectedIds.size === filtered.length;

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex items-center gap-2">
            <Button
              variant={showFilters ? "default" : "outline"}
              size="sm"
              className="gap-1"
              onClick={() => setShowFilters(!showFilters)}
            >
              <Filter className="h-3.5 w-3.5" />
              {t("filters.title")}
              {activeFilterCount > 0 && (
                <Badge variant="secondary" className="ml-1 h-4 min-w-[18px] px-1 text-[10px]">
                  {activeFilterCount}
                </Badge>
              )}
            </Button>
            <Button variant="outline" size="sm" onClick={handleRefresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      {showFilters && (
        <div className="mt-3 rounded-lg border bg-card p-3 space-y-3">
          {/* Server-side filters row */}
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
            {/* Status */}
            <select
              value={filterProcessed}
              onChange={(e) => setFilterProcessed(e.target.value as "all" | "pending" | "processed")}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm"
            >
              <option value="all">{t("filters.all")}</option>
              <option value="pending">{t("filters.pending")}</option>
              <option value="processed">{t("filters.processed")}</option>
            </select>

            {/* Agent dropdown */}
            <Select value={filterAgentId} onValueChange={setFilterAgentId}>
              <SelectTrigger className="h-8 text-sm">
                <SelectValue placeholder={t("filters.allAgents")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">{t("filters.allAgents")}</SelectItem>
                {agents.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.display_name || a.agent_key || a.id.slice(0, 8)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {/* Channel */}
            <input
              type="text"
              placeholder={t("filters.channel")}
              value={filterChannel}
              onChange={(e) => handleChannelChange(e.target.value)}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />

            {/* Graph ID */}
            <input
              type="text"
              placeholder={t("filters.graphId")}
              value={filterGraphId}
              onChange={(e) => handleGraphIdChange(e.target.value)}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />
          </div>

          {/* Client-side search row */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
            <input
              type="text"
              placeholder={t("filters.searchChat")}
              value={searchChat}
              onChange={(e) => setSearchChat(e.target.value)}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />
            <input
              type="text"
              placeholder={t("filters.searchSender")}
              value={searchSender}
              onChange={(e) => setSearchSender(e.target.value)}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />
            <input
              type="text"
              placeholder={t("filters.searchBody")}
              value={searchBody}
              onChange={(e) => setSearchBody(e.target.value)}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />
          </div>

          {/* Active filter chips */}
          {activeFilterCount > 0 && (
            <div className="flex flex-wrap items-center gap-2">
              {filterProcessed !== "all" && (
                <Badge variant="secondary" className="gap-1 text-xs">
                  {t("filters.status")}: {t(`filters.${filterProcessed}`)}
                  <button onClick={() => setFilterProcessed("all")} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
              {filterAgentId !== "__all__" && (
                <Badge variant="secondary" className="gap-1 text-xs">
                  {t("filters.agent")}: {agents.find((a) => a.id === filterAgentId)?.display_name || filterAgentId.slice(0, 8)}
                  <button onClick={() => setFilterAgentId("__all__")} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
              {filterChannel && (
                <Badge variant="secondary" className="gap-1 text-xs">
                  {t("filters.channel")}: {filterChannel}
                  <button onClick={() => { setFilterChannel(""); setOffset(0); }} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
              {filterGraphId && (
                <Badge variant="secondary" className="gap-1 text-xs">
                  {t("filters.graphId")}: {filterGraphId}
                  <button onClick={() => { setFilterGraphId(""); setOffset(0); }} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
              <Button size="sm" variant="ghost" className="h-5 px-2 text-xs text-muted-foreground" onClick={handleClearFilters}>
                {t("filters.clearAll")}
              </Button>
            </div>
          )}
        </div>
      )}

      {/* Selection toolbar */}
      {selectedIds.size > 0 && (
        <div className="mt-3 flex items-center gap-2 rounded-lg border bg-primary/5 px-3 py-2">
          <span className="text-xs text-muted-foreground">
            {selectedIds.size} selected
          </span>
          <Button
            size="sm"
            variant="outline"
            className="ml-auto h-7 gap-1 text-xs"
            onClick={handleResetToPending}
          >
            <RotateCcw className="h-3 w-3" />
            {t("actions.resetToPending")}
          </Button>
        </div>
      )}

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={6} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={FileText}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <>
            <div className="mb-2 text-xs text-muted-foreground">
              {t("showing", { count: filtered.length, total })}
            </div>
            <div className="overflow-x-auto rounded-md border">
              <table className="w-full min-w-[700px] text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="w-10 px-2 py-3">
                      <input
                        type="checkbox"
                        checked={allSelected}
                        onChange={toggleSelectAll}
                        className="h-4 w-4 rounded border-border"
                      />
                    </th>
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
                  {filtered.map((msg) => (
                    <tr
                      key={msg.id}
                      className={`cursor-pointer border-b last:border-0 hover:bg-muted/30 ${selectedIds.has(msg.id) ? "bg-primary/5" : ""}`}
                      onClick={() => setSelectedMsg(msg)}
                    >
                      <td className="px-2 py-3" onClick={(e) => e.stopPropagation()}>
                        <input
                          type="checkbox"
                          checked={selectedIds.has(msg.id)}
                          onChange={() => toggleSelect(msg.id)}
                          className="h-4 w-4 rounded border-border"
                        />
                      </td>
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
        <RawMessageDetailDialog
          message={selectedMsg}
          onClose={() => setSelectedMsg(null)}
          onReset={selectedMsg.processed_at ? () => handleResetSingle(selectedMsg) : undefined}
        />
      )}
    </div>
  );
}
