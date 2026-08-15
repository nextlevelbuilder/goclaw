import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Webhook as WebhookIcon, ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useDebounce } from "@/hooks/use-debounce";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";
import { useWebhooks } from "./hooks/use-webhooks";
import { WebhookListTable } from "./webhook-list-table";
import { WebhookFormDialog } from "./webhook-form-dialog";
import { WebhookSecretDialog, type WebhookSecretPayload } from "./webhook-secret-dialog";
import { WebhookTestDialog } from "./webhook-test-dialog";
import { WebhookCallsDialog } from "./webhook-calls-dialog";
import type { WebhookData } from "@/types/webhook";

export function WebhooksPage() {
  const { t } = useTranslation("webhooks");
  const { t: tc } = useTranslation("common");

  const PAGE_SIZE = 20;
  const [search, setSearch] = useState("");
  const [showRevoked, setShowRevoked] = useState(false);
  const [page, setPage] = useState(0);
  const debouncedSearch = useDebounce(search, 300);
  useEffect(() => {
    setPage(0);
  }, [debouncedSearch, showRevoked]);

  const { webhooks, total, loading, refresh, createWebhook, updateWebhook, rotateSecret, revokeWebhook, testWebhook } =
    useWebhooks({
      limit: PAGE_SIZE,
      offset: page * PAGE_SIZE,
      q: debouncedSearch || undefined,
      includeRevoked: showRevoked,
    });
  const { agents } = useAgents();
  const { instances } = useChannelInstances({ limit: 200 });

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && webhooks.length === 0);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const hasPrev = page > 0;
  const hasNext = (page + 1) * PAGE_SIZE < total;

  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<WebhookData | null>(null);
  const [secret, setSecret] = useState<WebhookSecretPayload | null>(null);
  const [testTarget, setTestTarget] = useState<WebhookData | null>(null);
  const [callsTarget, setCallsTarget] = useState<WebhookData | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<WebhookData | null>(null);
  const [rotateTarget, setRotateTarget] = useState<WebhookData | null>(null);
  const [actionLoading, setActionLoading] = useState(false);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const copyId = async (id: string) => {
    await navigator.clipboard.writeText(id);
    setCopiedId(id);
    setTimeout(() => setCopiedId((v) => (v === id ? null : v)), 2000);
  };

  const agentName = (id?: string) => (id ? agents.find((a) => a.id === id)?.display_name || agents.find((a) => a.id === id)?.agent_key || id.slice(0, 8) : "");
  const channelName = (id?: string) => (id ? instances.find((c) => c.id === id)?.display_name || instances.find((c) => c.id === id)?.name || id.slice(0, 8) : "");

  const openCreate = () => {
    setEditing(null);
    setFormOpen(true);
  };
  const openEdit = (w: WebhookData) => {
    setEditing(w);
    setFormOpen(true);
  };

  const handleRevoke = async () => {
    if (!revokeTarget) return;
    setActionLoading(true);
    try {
      await revokeWebhook(revokeTarget.id);
      setRevokeTarget(null);
    } finally {
      setActionLoading(false);
    }
  };

  const handleRotate = async () => {
    if (!rotateTarget) return;
    setActionLoading(true);
    try {
      const res = await rotateSecret(rotateTarget.id);
      const rid = rotateTarget.id;
      setRotateTarget(null);
      setSecret({ webhookId: rid, secret: res.secret, hmacSigningKey: res.hmac_signing_key });
    } finally {
      setActionLoading(false);
    }
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button size="sm" onClick={openCreate} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("addWebhook")}
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={spinning ? "animate-spin h-3.5 w-3.5" : "h-3.5 w-3.5"} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4 flex flex-wrap items-center gap-4">
        <SearchInput value={search} onChange={setSearch} placeholder={t("searchPlaceholder")} className="max-w-sm" />
        <div className="flex items-center gap-2">
          <Switch id="show-revoked" checked={showRevoked} onCheckedChange={setShowRevoked} />
          <Label htmlFor="show-revoked" className="text-sm text-muted-foreground">{t("showRevoked")}</Label>
        </div>
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : webhooks.length === 0 ? (
          page === 0 ? (
            <EmptyState icon={WebhookIcon} title={t("emptyTitle")} description={t("emptyDescription")} />
          ) : (
            <p className="py-8 text-center text-sm text-muted-foreground">{t("pager.noMore")}</p>
          )
        ) : (
          <WebhookListTable
            webhooks={webhooks}
            copiedId={copiedId}
            onCopyId={copyId}
            agentName={agentName}
            channelName={channelName}
            onTest={setTestTarget}
            onCalls={setCallsTarget}
            onEdit={openEdit}
            onRotate={setRotateTarget}
            onRevoke={setRevokeTarget}
          />
        )}

        {(hasPrev || hasNext) && (
          <div className="mt-3 flex items-center justify-between border-t pt-3">
            <span className="text-xs text-muted-foreground">
              {t("pager.pageOf", { page: page + 1, total: totalPages })}
            </span>
            <div className="flex items-center gap-1">
              <Button variant="outline" size="sm" onClick={() => setPage((p) => Math.max(0, p - 1))} disabled={!hasPrev || spinning} className="gap-1">
                <ChevronLeft className="h-3.5 w-3.5" /> {t("pager.prev")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setPage((p) => p + 1)} disabled={!hasNext || spinning} className="gap-1">
                {t("pager.next")} <ChevronRight className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        )}
      </div>

      <WebhookFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        editing={editing}
        onCreate={async (input) => {
          const res = await createWebhook(input);
          setSecret({ webhookId: res.id, secret: res.secret, hmacSigningKey: res.hmac_signing_key });
        }}
        onUpdate={updateWebhook}
      />

      <WebhookSecretDialog payload={secret} onClose={() => setSecret(null)} />

      <WebhookTestDialog webhook={testTarget} onClose={() => setTestTarget(null)} onRun={testWebhook} />

      <WebhookCallsDialog webhook={callsTarget} onClose={() => setCallsTarget(null)} />

      <ConfirmDeleteDialog
        open={!!revokeTarget}
        onOpenChange={(v) => !v && setRevokeTarget(null)}
        title={t("revoke.title")}
        description={t("revoke.description", { name: revokeTarget?.name })}
        confirmValue={revokeTarget?.name || ""}
        confirmLabel={t("actions.revoke")}
        onConfirm={handleRevoke}
        loading={actionLoading}
      />

      <ConfirmDeleteDialog
        open={!!rotateTarget}
        onOpenChange={(v) => !v && setRotateTarget(null)}
        title={t("rotate.title")}
        description={t("rotate.description", { name: rotateTarget?.name })}
        confirmValue={rotateTarget?.name || ""}
        confirmLabel={t("actions.rotate")}
        onConfirm={handleRotate}
        loading={actionLoading}
      />
    </div>
  );
}
