import { useState, useEffect } from "react";
import { useParams, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { Cpu, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useProviders, type ProviderData } from "./hooks/use-providers";
import { ProviderFormDialog } from "./provider-form-dialog";
import { ProviderListRow } from "./provider-list-row";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";
import { ProviderDetailPage } from "./provider-detail/provider-detail-page";

export function ProvidersPage() {
  const { id: detailId } = useParams<{ id: string }>();
  const navigate = useNavigate();

  if (detailId) {
    return (
      <ProviderDetailPage
        providerId={detailId}
        onBack={() => navigate("/providers")}
      />
    );
  }

  return <ProviderListView />;
}

function ProviderListView() {
  const { t } = useTranslation("providers");
  const navigate = useNavigate();

  const {
    providers, loading, refresh,
    createProvider, deleteProvider,
  } = useProviders();
  const showSkeleton = useDeferredLoading(loading && providers.length === 0);

  const [search, setSearch] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ProviderData | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  const filtered = providers.filter(
    (p) =>
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      (p.display_name || "").toLowerCase().includes(search.toLowerCase()),
  );

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, resetPage]);

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await deleteProvider(deleteTarget.id);
      setDeleteTarget(null);
    } finally {
      setDeleteLoading(false);
    }
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <Button onClick={() => setFormOpen(true)} className="gap-1">
            <Plus className="h-4 w-4" /> {t("addProvider")}
          </Button>
        }
      />

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-6">
        {showSkeleton ? (
          <TableSkeleton />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Cpu}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <>
            <div className="mt-4 flex flex-col gap-2">
              {pageItems.map((p) => (
                <ProviderListRow
                  key={p.id}
                  provider={p}
                  onClick={() => navigate(`/providers/${p.id}`)}
                  onDelete={() => setDeleteTarget(p)}
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

      <ProviderFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        onSubmit={async (data) => {
          await createProvider(data);
          refresh();
        }}
        existingProviders={providers}
      />

      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { name: deleteTarget?.display_name || deleteTarget?.name })}
        confirmValue={deleteTarget?.display_name || deleteTarget?.name || ""}
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
    </div>
  );
}
