import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Users, LayoutGrid, List } from "lucide-react";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { CardSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { Button } from "@/components/ui/button";
import { TooltipProvider, Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useTeams } from "./hooks/use-teams";
import { TeamCard } from "./team-card";
import { TeamListRow } from "./team-list-row";
import { TeamCreateDialog } from "./team-create-dialog";
import { usePagination } from "@/hooks/use-pagination";

interface TeamsListTabProps {
  onSelectTeam: (teamId: string) => void;
}

export function TeamsListTab({ onSelectTeam }: TeamsListTabProps) {
  const { t } = useTranslation("teams");
  const { t: tc } = useTranslation("common");
  const { teams, loading, load, createTeam, deleteTeam } = useTeams();
  const showSkeleton = useDeferredLoading(loading && teams.length === 0);

  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useState<"card" | "list">("card");
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; name: string } | null>(null);

  useEffect(() => { load(); }, [load]);

  const filtered = teams.filter((team) => {
    const q = search.toLowerCase();
    return team.name.toLowerCase().includes(q) || (team.description ?? "").toLowerCase().includes(q);
  });

  const { pageItems, pagination, setPage, setPageSize } = usePagination(filtered);

  return (
    <>
      {/* Toolbar: search + create + view toggle */}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <SearchInput value={search} onChange={setSearch} placeholder={t("searchPlaceholder")} className="max-w-sm" />
        <Button onClick={() => setCreateOpen(true)} className="gap-1 ml-auto sm:ml-0">
          <Plus className="h-4 w-4" /> {t("createTeam")}
        </Button>
        <TooltipProvider>
          <div className="sm:ml-auto flex items-center gap-0.5 rounded-md border p-0.5">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant={viewMode === "card" ? "default" : "ghost"} size="xs" className="h-7 w-7 p-0" onClick={() => setViewMode("card")}>
                  <LayoutGrid className="h-3.5 w-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("viewCard")}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant={viewMode === "list" ? "default" : "ghost"} size="xs" className="h-7 w-7 p-0" onClick={() => setViewMode("list")}>
                  <List className="h-3.5 w-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("viewList")}</TooltipContent>
            </Tooltip>
          </div>
        </TooltipProvider>
      </div>

      {/* Team grid/list */}
      <div className="mt-6">
        {showSkeleton ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => <CardSkeleton key={i} />)}
          </div>
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Users}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? tc("tryDifferentSearch") : t("emptyDescription")}
          />
        ) : (
          <>
            <TooltipProvider>
              {viewMode === "card" ? (
                <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                  {pageItems.map((team) => (
                    <TeamCard key={team.id} team={team} onClick={() => onSelectTeam(team.id)} onDelete={() => setDeleteTarget({ id: team.id, name: team.name })} />
                  ))}
                </div>
              ) : (
                <div className="flex flex-col gap-2">
                  {pageItems.map((team) => (
                    <TeamListRow key={team.id} team={team} onClick={() => onSelectTeam(team.id)} onDelete={() => setDeleteTarget({ id: team.id, name: team.name })} />
                  ))}
                </div>
              )}
            </TooltipProvider>
            <div className="mt-4">
              <Pagination page={pagination.page} pageSize={pagination.pageSize} total={pagination.total} totalPages={pagination.totalPages} onPageChange={setPage} onPageSizeChange={setPageSize} />
            </div>
          </>
        )}
      </div>

      <TeamCreateDialog open={createOpen} onOpenChange={setCreateOpen} onCreate={async (data) => { await createTeam(data); }} />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={() => setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { name: deleteTarget?.name })}
        confirmLabel={t("delete.confirmLabel")}
        variant="destructive"
        onConfirm={async () => { if (deleteTarget) { await deleteTeam(deleteTarget.id); setDeleteTarget(null); } }}
      />
    </>
  );
}
