import { useState, useRef } from "react";
import { useParams, useNavigate } from "react-router";
import { Radio, Plus, RefreshCw, Pencil, Trash2, QrCode } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
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
import { channelTypeLabels } from "./channels-status-view";
import { ChannelDetailPage } from "./channel-detail/channel-detail-page";
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
    return <ChannelDetailPage instanceId={detailId} onBack={() => navigate("/channels")} />;
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
    <div className="p-4 sm:p-6">
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

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={handleSearchChange}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : instances.length === 0 ? (
          <EmptyState
            icon={Radio}
            title={debouncedSearch ? t("noMatchTitle") : t("emptyTitle")}
            description={debouncedSearch ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.type")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.agent")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.enabled")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {instances.map((inst) => {
                  const status = getStatus(inst.name);
                  return (
                    <tr
                      key={inst.id}
                      className="border-b last:border-0 hover:bg-muted/30 cursor-pointer"
                      onClick={() => navigate(`/channels/${inst.id}`)}
                    >
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <Radio className="h-4 w-4 text-muted-foreground" />
                          <div>
                            <span className="font-medium">{inst.display_name || inst.name}</span>
                            {inst.display_name && (
                              <span className="ml-1 text-xs text-muted-foreground">({inst.name})</span>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <Badge variant="secondary">
                          {channelTypeLabels[inst.channel_type] || inst.channel_type}
                        </Badge>
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {getAgentName(inst.agent_id)}
                      </td>
                      <td className="px-4 py-3">
                        {status ? (
                          <div className="flex items-center gap-2">
                            <span
                              className={`h-2 w-2 rounded-full ${status.running ? "bg-green-500" : "bg-muted-foreground"}`}
                            />
                            <span className="text-muted-foreground">
                              {status.running ? t("status.running") : t("status.stopped")}
                            </span>
                          </div>
                        ) : (
                          <span className="text-xs text-muted-foreground">-</span>
                        )}
                      </td>
                      <td className="px-4 py-3">
                        <Badge variant={inst.enabled ? "default" : "secondary"}>
                          {inst.enabled ? t("enabled") : t("disabled")}
                        </Badge>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <div className="flex items-center justify-end gap-1">
                          {channelsWithAuth.has(inst.channel_type) && (
                            <Button
                              variant="ghost"
                              size="sm"
                              title={status?.running ? t("actions.reauthenticate") : t("actions.authenticate")}
                              onClick={(e) => { e.stopPropagation(); setQrTarget(inst); }}
                            >
                              <QrCode className="h-3.5 w-3.5" />
                            </Button>
                          )}
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={(e) => { e.stopPropagation(); setEditInstance(inst); setFormOpen(true); }}
                          >
                            <Pencil className="h-3.5 w-3.5" />
                          </Button>
                          {!inst.is_default && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={(e) => { e.stopPropagation(); setDeleteTarget(inst); }}
                              className="text-destructive hover:text-destructive"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <Pagination
              page={page}
              pageSize={pageSize}
              total={total}
              totalPages={totalPages}
              onPageChange={setPage}
              onPageSizeChange={(size) => { setPageSize(size); setPage(1); }}
            />
          </div>
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
              // Backend reload is async (~2-3s: stop → sleep → restart).
              // Refresh after reload has time to complete.
              setTimeout(() => refresh(), 3000);
            }}
          />
        ) : null;
      })()}
    </div>
  );
}
