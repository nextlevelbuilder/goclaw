import { useState, useEffect, useMemo } from "react";
import { useParams, useNavigate } from "react-router";
import { Plus, Bot, LayoutGrid, List, ArrowLeftRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { CardSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { Button } from "@/components/ui/button";
import { TooltipProvider, Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { useAgents } from "./hooks/use-agents";
import { AgentCard } from "./agent-card";
import { AgentListRow } from "./agent-list-row";
import { AgentCreateDialog } from "./agent-create-dialog";
import { AgentDetailPage } from "./agent-detail/agent-detail-page";
import { SummoningModal } from "./summoning-modal";
import { usePagination } from "@/hooks/use-pagination";

export function AgentsPage() {
  const { t } = useTranslation("agents");
  const { id: detailId } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { agents, loading, createAgent, deleteAgent, refresh, resummonAgent } = useAgents();
  const showSkeleton = useDeferredLoading(loading && agents.length === 0);

  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useState<"card" | "list">("card");
  const [ownerFilter, setOwnerFilter] = useState<string | undefined>();
  const [typeFilter, setTypeFilter] = useState<string | undefined>();
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; name: string } | null>(null);
  const [summoningAgent, setSummoningAgent] = useState<{ id: string; name: string } | null>(null);

  // Collect unique owner IDs for filter + contact resolution
  const ownerIDs = useMemo(() => [...new Set(agents.map((a) => a.owner_id).filter(Boolean))], [agents]);
  const { resolve } = useContactResolver(ownerIDs);

  const filtered = useMemo(() => agents.filter((a) => {
    if (ownerFilter && a.owner_id !== ownerFilter) return false;
    if (typeFilter && a.agent_type !== typeFilter) return false;
    const q = search.toLowerCase();
    return (
      a.agent_key.toLowerCase().includes(q) ||
      (a.display_name ?? "").toLowerCase().includes(q)
    );
  }), [agents, ownerFilter, typeFilter, search]);

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, ownerFilter, typeFilter, resetPage]);

  const handleResummon = async (agent: { id: string; display_name?: string; agent_key: string }) => {
    try {
      await resummonAgent(agent.id);
      setSummoningAgent({ id: agent.id, name: agent.display_name || agent.agent_key });
    } catch {
      // error handled by hook
    }
  };

  // Show detail view if route has :id
  if (detailId) {
    return (
      <AgentDetailPage
        agentId={detailId}
        onBack={() => navigate("/agents")}
      />
    );
  }

  const resolveOwnerName = (id: string) => {
    const contact = resolve(id);
    return contact?.display_name || contact?.username || id;
  };

  const handleClick = (agent: { id: string; display_name?: string; agent_key: string; status: string }) => {
    if (agent.status === "summoning") {
      setSummoningAgent({ id: agent.id, name: agent.display_name || agent.agent_key });
    } else {
      navigate(`/agents/${agent.id}`);
    }
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" onClick={() => navigate("/import-export?tab=agents")} className="gap-1">
              <ArrowLeftRight className="h-4 w-4" /> {t("transfer.title")}
            </Button>
            <Button onClick={() => setCreateOpen(true)} className="gap-1">
              <Plus className="h-4 w-4" /> {t("createAgent")}
            </Button>
          </div>
        }
      />

      {/* Toolbar: search + creator filter + view toggle */}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />

        {/* Type filter */}
        <Select
          value={typeFilter ?? "__all__"}
          onValueChange={(v) => setTypeFilter(v === "__all__" ? undefined : v)}
        >
          <SelectTrigger className="h-9 w-36 text-xs">
            <SelectValue placeholder={t("allTypes", "All Types")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{t("allTypes", "All Types")}</SelectItem>
            <SelectItem value="open">{t("typeOpen", "Open")}</SelectItem>
            <SelectItem value="predefined">{t("typePredefined", "Predefined")}</SelectItem>
          </SelectContent>
        </Select>

        {/* Creator filter */}
        {ownerIDs.length > 0 && (
          <Select
            value={ownerFilter ?? "__all__"}
            onValueChange={(v) => setOwnerFilter(v === "__all__" ? undefined : v)}
          >
            <SelectTrigger className="h-9 w-44 text-xs">
              <SelectValue placeholder={t("allCreators")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{t("allCreators")}</SelectItem>
              {ownerIDs.map((id) => (
                <SelectItem key={id} value={id}>
                  {resolveOwnerName(id)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}

        {/* View toggle */}
        <TooltipProvider>
          <div className="ml-auto flex items-center gap-0.5 rounded-md border p-0.5">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant={viewMode === "card" ? "default" : "ghost"}
                  size="xs"
                  className="h-7 w-7 p-0"
                  onClick={() => setViewMode("card")}
                >
                  <LayoutGrid className="h-3.5 w-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("viewCard")}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant={viewMode === "list" ? "default" : "ghost"}
                  size="xs"
                  className="h-7 w-7 p-0"
                  onClick={() => setViewMode("list")}
                >
                  <List className="h-3.5 w-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("viewList")}</TooltipContent>
            </Tooltip>
          </div>
        </TooltipProvider>
      </div>

      <div className="mt-6">
        {showSkeleton ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <CardSkeleton key={i} />
            ))}
          </div>
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Bot}
            title={search || ownerFilter || typeFilter ? t("noMatchTitle") : t("emptyTitle")}
            description={
              search || ownerFilter || typeFilter
                ? t("noMatchDescription")
                : t("emptyDescription")
            }
          />
        ) : (
          <>
            <TooltipProvider>
              {viewMode === "card" ? (
                <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                  {pageItems.map((agent) => (
                    <AgentCard
                      key={agent.id}
                      agent={agent}
                      onClick={() => handleClick(agent)}
                      onResummon={() => handleResummon(agent)}
                      onDelete={() => setDeleteTarget({ id: agent.id, name: agent.display_name || agent.agent_key })}
                    />
                  ))}
                </div>
              ) : (
                <div className="flex flex-col gap-2">
                  {pageItems.map((agent) => (
                    <AgentListRow
                      key={agent.id}
                      agent={agent}
                      ownerName={resolveOwnerName(agent.owner_id)}
                      onClick={() => handleClick(agent)}
                      onResummon={() => handleResummon(agent)}
                      onDelete={() => setDeleteTarget({ id: agent.id, name: agent.display_name || agent.agent_key })}
                    />
                  ))}
                </div>
              )}
            </TooltipProvider>
            <div className="mt-4">
              <Pagination
                page={pagination.page}
                pageSize={pagination.pageSize}
                total={pagination.total}
                totalPages={pagination.totalPages}
                onPageChange={setPage}
                onPageSizeChange={setPageSize}
              />
            </div>
          </>
        )}
      </div>

      <AgentCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreate={async (data) => {
          const created = await createAgent(data);
          refresh();
          if (created && typeof created === "object" && "status" in created && created.status === "summoning") {
            const ag = created as { id: string; display_name?: string; agent_key: string; other_config?: Record<string, unknown> };
            // Skip summoning modal for none/minimal — these modes don't use persona files
            const pm = ag.other_config?.prompt_mode;
            if (pm === "none" || pm === "minimal") {
              // no-op: summoning will still run on backend but we skip the modal
            } else {
              setSummoningAgent({ id: ag.id, name: ag.display_name || ag.agent_key });
            }
          }
        }}
      />

      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.deleteWarning")}
        confirmValue={deleteTarget?.name ?? ""}
        onConfirm={async () => {
          if (deleteTarget) {
            await deleteAgent(deleteTarget.id);
            setDeleteTarget(null);
          }
        }}
      />

      {summoningAgent && (
        <SummoningModal
          open={!!summoningAgent}
          onOpenChange={(open) => { if (!open) setSummoningAgent(null); }}
          agentId={summoningAgent.id}
          agentName={summoningAgent.name}
          onCompleted={refresh}
          onResummon={resummonAgent}
        />
      )}
    </div>
  );
}
