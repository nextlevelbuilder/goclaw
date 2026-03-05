import { useState, useEffect } from "react";
import { useParams, useNavigate } from "react-router";
import { Plus, Users } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { CardSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useTeams } from "./hooks/use-teams";
import { TeamCard } from "./team-card";
import { TeamCreateDialog } from "./team-create-dialog";
import { TeamDetailPage } from "./team-detail-page";
import { usePagination } from "@/hooks/use-pagination";

export function TeamsPage() {
  const { id: detailId } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { teams, loading, load, createTeam, deleteTeam } = useTeams();
  const showSkeleton = useDeferredLoading(loading && teams.length === 0);

  const [search, setSearch] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; name: string } | null>(null);

  useEffect(() => {
    load();
  }, [load]);

  // Show detail view if route has :id
  if (detailId) {
    return (
      <TeamDetailPage
        teamId={detailId}
        onBack={() => navigate("/teams")}
      />
    );
  }

  const filtered = teams.filter((t) => {
    const q = search.toLowerCase();
    return (
      t.name.toLowerCase().includes(q) ||
      (t.description ?? "").toLowerCase().includes(q)
    );
  });

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, resetPage]);

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Teams"
        description="Manage your agent teams"
        actions={
          <Button onClick={() => setCreateOpen(true)} className="gap-1">
            <Plus className="h-4 w-4" /> Create Team
          </Button>
        }
      />

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder="Search teams..."
          className="max-w-sm"
        />
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
            icon={Users}
            title={search ? "No matching teams" : "No teams yet"}
            description={
              search
                ? "Try a different search term."
                : "Create your first team to get started."
            }
          />
        ) : (
          <>
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {pageItems.map((team) => (
                <TeamCard
                  key={team.id}
                  team={team}
                  onClick={() => navigate(`/teams/${team.id}`)}
                />
              ))}
            </div>
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

      <TeamCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreate={async (data) => {
          await createTeam(data);
        }}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={() => setDeleteTarget(null)}
        title="Delete Team"
        description={`Are you sure you want to delete "${deleteTarget?.name}"? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={async () => {
          if (deleteTarget) {
            await deleteTeam(deleteTarget.id);
            setDeleteTarget(null);
          }
        }}
      />
    </div>
  );
}
