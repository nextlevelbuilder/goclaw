import { useState, useRef } from "react";
import { useParams, useNavigate } from "react-router";
import { Radio, Plus, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useChannels } from "./hooks/use-channels";
import { useChannelInstances, type ChannelInstanceData, type ChannelInstanceInput } from "./hooks/use-channel-instances";
import { ChannelInstanceFormDialog } from "./channel-instance-form-dialog";
import { channelsWithAuth, reauthDialogs } from "./channel-wizard-registry";
import { ChannelDetailPage } from "./channel-detail/channel-detail-page";
import { ChannelListRow } from "./channel-list-row";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";

export function ChannelsPage() {
  const { t } = useTranslation("channels");
  const { id: detailId } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const { channels, loading: statusLoading, refresh: refreshStatus } = useChannels();

  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [formOpen, setFormOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ChannelInstanceData | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [editInstance, setEditInstance] = useState<ChannelInstanceData | null>(null);
  const [qrTarget, setQrTarget] = useState<ChannelInstanceData | null>(null);

  const pendingSearchRef = useRef("");
  const flushSearch = useDebouncedCallback(() => {
    setDebouncedSearch(pendingSearchRef.current);
    setPage(1);
  }, 300);

  const handleSearchChange = (v: string) => {
    setSearch(v);
    pendingSearchRef.current = v;
    flushSearch();
  };

  const {
    instances, total, loading: instancesLoading,
    refresh: refreshInstances, createInstance, updateInstance, deleteInstance,
  } = useChannelInstances({
    search: debouncedSearch || undefined,
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });
  const { agents } = useAgents();

  const loading = statusLoading || instancesLoading;
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && instances.length === 0);
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  const refresh = () => {
    refreshStatus();
    refreshInstances();
  };

  // Detail view
  if (detailId) {
    return (
      <ChannelDetailPage
        instanceId={detailId}
        onBack={() => navigate("/channels")}
        onDelete={async ({ id }) => {
          await deleteInstance(id);
          navigate("/channels");
        }}
      />
    );
  }

  const handleCreate = async (data: ChannelInstanceInput) => {
    return await createInstance(data);
  };

  const handleEdit = async (data: ChannelInstanceInput) => {
    if (!editInstance) return;
    await updateInstance(editInstance.id, data);
  };

  const handleUpdate = async (id: string, data: Partial<ChannelInstanceInput>) => {
    await updateInstance(id, data);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await deleteInstance(deleteTarget.id);
      setDeleteTarget(null);
    } finally {
      setDeleteLoading(false);
    }
  };

  const getAgentName = (agentId: string) => {
    const agent = agents.find((a) => a.id === agentId);
    return agent?.display_name || agent?.agent_key || agentId.slice(0, 8);
  };

  const getStatus = (instanceName: string) => {
    return channels[instanceName] ?? null;
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button size="sm" onClick={() => setFormOpen(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("addChannel")}
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {t("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <SearchInput
          value={search}
          onChange={handleSearchChange}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton />
        ) : instances.length === 0 ? (
          <EmptyState
            icon={Radio}
            title={debouncedSearch ? t("noMatchTitle") : t("emptyTitle")}
            description={debouncedSearch ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <>
            <div className="mt-4 flex flex-col gap-2">
              {instances.map((inst) => (
                <ChannelListRow
                  key={inst.id}
                  instance={inst}
                  status={getStatus(inst.name)}
                  agentName={getAgentName(inst.agent_id)}
                  onClick={() => navigate(`/channels/${inst.id}`)}
                  onAuth={channelsWithAuth.has(inst.channel_type) ? () => setQrTarget(inst) : undefined}
                  onDelete={!inst.is_default ? () => setDeleteTarget(inst) : undefined}
                />
              ))}
            </div>
            <Pagination
              page={page}
              pageSize={pageSize}
              total={total}
              totalPages={totalPages}
              onPageChange={setPage}
              onPageSizeChange={(size) => { setPageSize(size); setPage(1); }}
            />
          </>
        )}
      </div>

      <ChannelInstanceFormDialog
        open={formOpen}
        onOpenChange={(open) => {
          setFormOpen(open);
          if (!open) {
            setEditInstance(null);
            setTimeout(() => refresh(), 1500);
          }
        }}
        instance={editInstance}
        agents={agents}
        onSubmit={editInstance ? handleEdit : handleCreate}
        onUpdate={handleUpdate}
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

      {qrTarget && (() => {
        const AuthDialog = reauthDialogs[qrTarget.channel_type];
        return AuthDialog ? (
          <AuthDialog
            open={!!qrTarget}
            onOpenChange={(v) => !v && setQrTarget(null)}
            instanceId={qrTarget.id}
            instanceName={qrTarget.display_name || qrTarget.name}
            onSuccess={() => {
              setQrTarget(null);
              setTimeout(() => refresh(), 3000);
            }}
          />
        ) : null;
      })()}
    </div>
  );
}
