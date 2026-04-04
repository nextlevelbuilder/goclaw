import { useEffect, useMemo, useState } from "react";
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
import { useChatGPTOAuthProviderQuotas } from "./hooks/use-chatgpt-oauth-provider-quotas";
import { useChatGPTOAuthProviderStatuses } from "./hooks/use-chatgpt-oauth-provider-statuses";
import { ProviderFormDialog } from "./provider-form-dialog";
import { ProviderListRow } from "./provider-list-row";
import { PoolSetupWizardDialog } from "./pool-setup-wizard-dialog";
import {
  getChatGPTOAuthPoolOwnership,
  sortProvidersForPoolHierarchy,
} from "./provider-utils";
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
    createProvider, updateProvider, deleteProvider,
  } = useProviders();
  const showSkeleton = useDeferredLoading(loading && providers.length === 0);
  const { statuses } = useChatGPTOAuthProviderStatuses(providers);

  const [search, setSearch] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [wizardOpen, setWizardOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ProviderData | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const providerByName = useMemo(
    () => new Map(providers.map((provider) => [provider.name, provider])),
    [providers],
  );
  const poolOwnership = useMemo(
    () => getChatGPTOAuthPoolOwnership(providers),
    [providers],
  );
  const oauthAvailabilityByName = useMemo(
    () => new Map(statuses.map((status) => [status.provider.name, status.availability])),
    [statuses],
  );
  // Unpooled = chatgpt_oauth providers that are neither pool owners nor members
  const unpooledProviders = useMemo(
    () => providers.filter(
      (p) =>
        p.provider_type === "chatgpt_oauth" &&
        !poolOwnership.membersByOwner.has(p.name) &&
        !poolOwnership.ownerByMember.has(p.name),
    ),
    [providers, poolOwnership],
  );

  const filtered = useMemo(() => providers.filter(
    (provider) =>
      provider.name.toLowerCase().includes(search.toLowerCase()) ||
      (provider.display_name || "").toLowerCase().includes(search.toLowerCase()),
  ), [providers, search]);
  const orderedProviders = useMemo(
    () => sortProvidersForPoolHierarchy(filtered, poolOwnership),
    [filtered, poolOwnership],
  );
  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(orderedProviders);
  const memberConnectorByName = useMemo(() => {
    const visibleNames = new Set(pageItems.map((provider) => provider.name));
    const map = new Map<string, "none" | "single" | "first" | "middle" | "last">();

    for (const [ownerName] of poolOwnership.membersByOwner) {
      if (!visibleNames.has(ownerName)) continue;

      const visibleMembers = pageItems
        .filter((provider) => poolOwnership.ownerByMember.get(provider.name) === ownerName)
        .map((provider) => provider.name);

      if (visibleMembers.length === 1) {
        const onlyMember = visibleMembers[0];
        if (onlyMember) {
          map.set(onlyMember, "single");
        }
        continue;
      }

      visibleMembers.forEach((name, index) => {
        if (index === 0) {
          map.set(name, "first");
        } else if (index === visibleMembers.length - 1) {
          map.set(name, "last");
        } else {
          map.set(name, "middle");
        }
      });
    }

    return map;
  }, [pageItems, poolOwnership.membersByOwner, poolOwnership.ownerByMember]);
  const visibleQuotaProviderNames = useMemo(
    () =>
      pageItems
        .filter(
          (provider) =>
            provider.provider_type === "chatgpt_oauth" &&
            oauthAvailabilityByName.get(provider.name) === "ready",
        )
        .map((provider) => provider.name),
    [oauthAvailabilityByName, pageItems],
  );
  const {
    quotaByName,
    isLoading: quotasLoading,
    isFetching: quotasFetching,
  } = useChatGPTOAuthProviderQuotas(visibleQuotaProviderNames, visibleQuotaProviderNames.length > 0);

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
                  oauthPool={p.provider_type === "chatgpt_oauth" ? {
                    availability: oauthAvailabilityByName.get(p.name) ?? (p.enabled ? "needs_sign_in" : "disabled"),
                    role: poolOwnership.ownerByMember.has(p.name)
                      ? "member"
                      : poolOwnership.membersByOwner.has(p.name)
                        ? "owner"
                        : "standalone",
                    managedByLabel: (() => {
                      const ownerName = poolOwnership.ownerByMember.get(p.name);
                      if (!ownerName) return undefined;
                      const owner = providerByName.get(ownerName);
                      return owner?.display_name || owner?.name || ownerName;
                    })(),
                    memberCount: poolOwnership.membersByOwner.get(p.name)?.length ?? 0,
                    strategy: poolOwnership.strategyByOwner.get(p.name) ?? "primary_first",
                    connectorPosition: memberConnectorByName.get(p.name) ?? "none",
                    quota: quotaByName.get(p.name),
                    quotaLoading: oauthAvailabilityByName.get(p.name) === "ready"
                      ? quotasLoading || quotasFetching
                      : false,
                  } : undefined}
                  showPoolHint={
                    p.provider_type === "chatgpt_oauth" &&
                    !poolOwnership.ownerByMember.has(p.name) &&
                    !poolOwnership.membersByOwner.has(p.name) &&
                    unpooledProviders.length >= 2
                  }
                  onClick={() => navigate(`/providers/${p.id}`)}
                  onDelete={() => setDeleteTarget(p)}
                  onPoolSetup={() => setWizardOpen(true)}
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

      <PoolSetupWizardDialog
        open={wizardOpen}
        onOpenChange={setWizardOpen}
        providers={providers}
        unpooledProviders={unpooledProviders}
        onSave={async (ownerId, data) => {
          await updateProvider(ownerId, data);
          refresh();
        }}
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
