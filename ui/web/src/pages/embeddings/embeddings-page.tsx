import { useState, useEffect, useMemo, useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Layers, RefreshCw, X, Filter, Trash2, Wand2 } from "lucide-react";
import { toast } from "@/stores/use-toast-store";
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
import { useEmbeddings } from "./hooks/use-embeddings";
import type { EmbeddingChunk } from "./hooks/use-embeddings";
import { EmbeddingDetailDialog } from "./embedding-detail-dialog";

const PAGE_SIZE = 50;

export function EmbeddingsPage() {
  const { t } = useTranslation("embeddings");
  const { t: tc } = useTranslation("common");
  const { agents } = useAgents();
  const { chunks, total, loading, loadChunks, deleteChunks, reEmbed } = useEmbeddings();

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && chunks.length === 0);
  const [offset, setOffset] = useState(0);
  const [showFilters, setShowFilters] = useState(false);
  const [selectedChunk, setSelectedChunk] = useState<EmbeddingChunk | null>(null);

  // Server-side filters
  const [filterAgentId, setFilterAgentId] = useState<string>("__all__");
  const [filterChatId, setFilterChatId] = useState("");
  const [filterGraphId, setFilterGraphId] = useState("");
  const [filterSender, setFilterSender] = useState("");
  const [filterEmbedding, setFilterEmbedding] = useState<"all" | "yes" | "no">("all");
  const [filterFromTime, setFilterFromTime] = useState("");
  const [filterToTime, setFilterToTime] = useState("");

  // Client-side text search
  const [searchText, setSearchText] = useState("");

  // Row selection
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [reEmbedding, setReEmbedding] = useState(false);

  // Debounced text inputs
  const chatTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const graphTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const senderTimer = useRef<ReturnType<typeof setTimeout>>(undefined);

  const handleChatIdChange = useCallback((v: string) => {
    setFilterChatId(v);
    clearTimeout(chatTimer.current);
    chatTimer.current = setTimeout(() => setOffset(0), 400);
  }, []);

  const handleGraphIdChange = useCallback((v: string) => {
    setFilterGraphId(v);
    clearTimeout(graphTimer.current);
    graphTimer.current = setTimeout(() => setOffset(0), 400);
  }, []);

  const handleSenderChange = useCallback((v: string) => {
    setFilterSender(v);
    clearTimeout(senderTimer.current);
    senderTimer.current = setTimeout(() => setOffset(0), 400);
  }, []);

  // Reset offset when server-side filters change
  useEffect(() => { setOffset(0); setSelectedIds(new Set()); }, [filterAgentId, filterEmbedding, filterFromTime, filterToTime]);

  // Fetch data
  useEffect(() => {
    const params: {
      agentId?: string;
      chatId?: string;
      graphId?: string;
      sender?: string;
      hasEmbedding?: boolean;
      fromTime?: string;
      toTime?: string;
      limit: number;
      offset: number;
    } = { limit: PAGE_SIZE, offset };
    if (filterAgentId !== "__all__") params.agentId = filterAgentId;
    if (filterChatId) params.chatId = filterChatId;
    if (filterGraphId) params.graphId = filterGraphId;
    if (filterSender) params.sender = filterSender;
    if (filterEmbedding === "yes") params.hasEmbedding = true;
    if (filterEmbedding === "no") params.hasEmbedding = false;
    if (filterFromTime) params.fromTime = new Date(filterFromTime).toISOString();
    if (filterToTime) params.toTime = new Date(filterToTime + "T23:59:59").toISOString();
    loadChunks(params);
  }, [offset, filterAgentId, filterChatId, filterGraphId, filterSender, filterEmbedding, filterFromTime, filterToTime, loadChunks]);

  // Client-side filtered chunks
  const filtered = useMemo(() => {
    if (!searchText) return chunks;
    const q = searchText.toLowerCase();
    return chunks.filter((c) => c.text.toLowerCase().includes(q));
  }, [chunks, searchText]);

  // Active filter count
  const activeFilterCount = useMemo(() => {
    let count = 0;
    if (filterAgentId !== "__all__") count++;
    if (filterChatId) count++;
    if (filterGraphId) count++;
    if (filterSender) count++;
    if (filterEmbedding !== "all") count++;
    if (filterFromTime || filterToTime) count++;
    return count;
  }, [filterAgentId, filterChatId, filterGraphId, filterSender, filterEmbedding, filterFromTime, filterToTime]);

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
      return new Set(filtered.map((c) => c.id));
    });
  }, [filtered]);

  // Clear selection when data changes
  useEffect(() => {
    setSelectedIds((prev) => {
      const ids = new Set<string>();
      for (const id of prev) {
        if (filtered.some((c) => c.id === id)) ids.add(id);
      }
      return ids.size === prev.size ? prev : ids;
    });
  }, [filtered]);

  const handleClearFilters = () => {
    setFilterAgentId("__all__");
    setFilterChatId("");
    setFilterGraphId("");
    setFilterSender("");
    setFilterEmbedding("all");
    setFilterFromTime("");
    setFilterToTime("");
    setSearchText("");
    setOffset(0);
  };

  const handleRefresh = () => {
    loadChunks({
      agentId: filterAgentId !== "__all__" ? filterAgentId : undefined,
      chatId: filterChatId || undefined,
      graphId: filterGraphId || undefined,
      sender: filterSender || undefined,
      hasEmbedding: filterEmbedding === "yes" ? true : filterEmbedding === "no" ? false : undefined,
      fromTime: filterFromTime ? new Date(filterFromTime).toISOString() : undefined,
      toTime: filterToTime ? new Date(filterToTime + "T23:59:59").toISOString() : undefined,
      limit: PAGE_SIZE,
      offset,
    });
  };

  const handleDeleteSelected = async () => {
    if (selectedIds.size === 0) return;
    try {
      const count = await deleteChunks([...selectedIds]);
      setSelectedIds(new Set());
      handleRefresh();
      toast.success(t("actions.deleteSuccess", { count }));
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t("actions.deleteFailed"), msg);
    }
  };

  const handleDeleteSingle = async (chunk: EmbeddingChunk) => {
    try {
      await deleteChunks([chunk.id]);
      setSelectedChunk(null);
      handleRefresh();
      toast.success(t("actions.deleteSuccess", { count: 1 }));
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t("actions.deleteFailed"), msg);
    }
  };

  const handleReEmbed = async () => {
    setReEmbedding(true);
    try {
      const result = await reEmbed({
        agentId: filterAgentId !== "__all__" ? filterAgentId : undefined,
        chatId: filterChatId || undefined,
        graphId: filterGraphId || undefined,
      });
      handleRefresh();
      if (result.processed === 0) {
        toast.info(t("actions.reEmbedSuccess", { count: 0 }));
      } else if (result.failed > 0) {
        toast.warning(t("actions.reEmbedPartial", { processed: result.processed }), t("actions.reEmbedPartialDetail", { failed: result.failed }));
      } else {
        toast.success(t("actions.reEmbedSuccess", { count: result.processed }));
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t("actions.reEmbedFailed"), msg);
    } finally {
      setReEmbedding(false);
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
            <Button
              variant="outline"
              size="sm"
              className="gap-1 min-h-[44px] sm:min-h-0"
              onClick={handleReEmbed}
              disabled={reEmbedding || spinning}
            >
              <Wand2 className={"h-3.5 w-3.5" + (reEmbedding ? " animate-pulse" : "")} />
              {reEmbedding ? t("actions.reEmbedding") : t("actions.reEmbed")}
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
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
            {/* Embedding status */}
            <select
              value={filterEmbedding}
              onChange={(e) => setFilterEmbedding(e.target.value as "all" | "yes" | "no")}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm"
            >
              <option value="all">{t("filters.all")}</option>
              <option value="yes">{t("filters.hasEmbedding")}</option>
              <option value="no">{t("filters.noEmbedding")}</option>
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

            {/* Chat ID */}
            <input
              type="text"
              placeholder={t("filters.chatId")}
              value={filterChatId}
              onChange={(e) => handleChatIdChange(e.target.value)}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
            {/* Graph ID */}
            <input
              type="text"
              placeholder={t("filters.graphId")}
              value={filterGraphId}
              onChange={(e) => handleGraphIdChange(e.target.value)}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />

            {/* Sender */}
            <input
              type="text"
              placeholder={t("filters.sender")}
              value={filterSender}
              onChange={(e) => handleSenderChange(e.target.value)}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />

            {/* Date from */}
            <input
              type="date"
              value={filterFromTime}
              onChange={(e) => { setFilterFromTime(e.target.value); setOffset(0); }}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />

            {/* Date to */}
            <input
              type="date"
              value={filterToTime}
              onChange={(e) => { setFilterToTime(e.target.value); setOffset(0); }}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />
          </div>

          {/* Client-side search */}
          <div className="grid grid-cols-1 gap-2">
            <input
              type="text"
              placeholder={t("filters.searchText")}
              value={searchText}
              onChange={(e) => setSearchText(e.target.value)}
              className="h-8 rounded-md border bg-background px-2 text-sm text-base md:text-sm placeholder:text-muted-foreground"
            />
          </div>

          {/* Active filter chips */}
          {activeFilterCount > 0 && (
            <div className="flex flex-wrap items-center gap-2">
              {filterEmbedding !== "all" && (
                <Badge variant="secondary" className="gap-1 text-xs">
                  {t("filters.embedding")}: {filterEmbedding === "yes" ? t("filters.hasEmbedding") : t("filters.noEmbedding")}
                  <button onClick={() => setFilterEmbedding("all")} className="ml-0.5 hover:text-foreground">
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
              {filterChatId && (
                <Badge variant="secondary" className="gap-1 text-xs">
                  {t("filters.chatId")}: {filterChatId}
                  <button onClick={() => { setFilterChatId(""); setOffset(0); }} className="ml-0.5 hover:text-foreground">
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
              {filterSender && (
                <Badge variant="secondary" className="gap-1 text-xs">
                  {t("filters.sender")}: {filterSender}
                  <button onClick={() => { setFilterSender(""); setOffset(0); }} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
              {(filterFromTime || filterToTime) && (
                <Badge variant="secondary" className="gap-1 text-xs">
                  {t("filters.dateRange")}
                  <button onClick={() => { setFilterFromTime(""); setFilterToTime(""); setOffset(0); }} className="ml-0.5 hover:text-foreground">
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
            className="ml-auto h-7 gap-1 text-xs text-destructive hover:text-destructive"
            onClick={handleDeleteSelected}
          >
            <Trash2 className="h-3 w-3" />
            {t("actions.deleteSelected")}
          </Button>
        </div>
      )}

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={6} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Layers}
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
                    <th className="px-4 py-3 text-left font-medium">{t("columns.text")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.chunkIndex")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.embedding")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.msgTime")}</th>
                    <th className="px-4 py-3 text-left font-medium">{t("columns.createdAt")}</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((chunk) => (
                    <tr
                      key={chunk.id}
                      className={`cursor-pointer border-b last:border-0 hover:bg-muted/30 ${selectedIds.has(chunk.id) ? "bg-primary/5" : ""}`}
                      onClick={() => setSelectedChunk(chunk)}
                    >
                      <td className="px-2 py-3" onClick={(e) => e.stopPropagation()}>
                        <input
                          type="checkbox"
                          checked={selectedIds.has(chunk.id)}
                          onChange={() => toggleSelect(chunk.id)}
                          className="h-4 w-4 rounded border-border"
                        />
                      </td>
                      <td className="max-w-[160px] truncate px-4 py-3 font-medium">
                        {chunk.chat_name || chunk.chat_id}
                      </td>
                      <td className="max-w-[140px] truncate px-4 py-3">
                        {chunk.sender}
                      </td>
                      <td className="max-w-[300px] truncate px-4 py-3 text-muted-foreground">
                        {chunk.text}
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {chunk.chunk_index}
                      </td>
                      <td className="px-4 py-3">
                        {chunk.has_embedding ? (
                          <Badge variant="success" className="text-xs">{t("status.embedded")}</Badge>
                        ) : (
                          <Badge variant="secondary" className="text-xs">{t("status.noVector")}</Badge>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-muted-foreground">
                        {formatDate(chunk.msg_time_from)}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-muted-foreground">
                        {formatDate(chunk.created_at)}
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

      {selectedChunk && (
        <EmbeddingDetailDialog
          chunk={selectedChunk}
          onClose={() => setSelectedChunk(null)}
          onDelete={() => handleDeleteSingle(selectedChunk)}
        />
      )}
    </div>
  );
}
