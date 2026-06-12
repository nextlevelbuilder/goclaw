import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Webhook as WebhookIcon, Play, History, Pencil, KeyRound, Ban, Bot, Radio, Copy, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";
import { formatRelativeTime } from "@/lib/format";
import { useWebhooks } from "./hooks/use-webhooks";
import { WebhookFormDialog } from "./webhook-form-dialog";
import { WebhookSecretDialog, type WebhookSecretPayload } from "./webhook-secret-dialog";
import { WebhookTestDialog } from "./webhook-test-dialog";
import { WebhookCallsDialog } from "./webhook-calls-dialog";
import type { WebhookData } from "@/types/webhook";

export function WebhooksPage() {
  const { t } = useTranslation("webhooks");
  const { t: tc } = useTranslation("common");
  const { webhooks, loading, refresh, createWebhook, updateWebhook, rotateSecret, revokeWebhook, testWebhook } = useWebhooks();
  const { agents } = useAgents();
  const { instances } = useChannelInstances({ limit: 200 });

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && webhooks.length === 0);

  const [search, setSearch] = useState("");
  const [showRevoked, setShowRevoked] = useState(false);
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

  const filtered = useMemo(
    () =>
      webhooks
        .filter((w) => (showRevoked ? true : !w.revoked))
        .filter((w) => w.name.toLowerCase().includes(search.toLowerCase()) || w.secret_prefix.includes(search)),
    [webhooks, showRevoked, search],
  );

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
        ) : filtered.length === 0 ? (
          <EmptyState icon={WebhookIcon} title={t("emptyTitle")} description={t("emptyDescription")} />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[800px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.webhookId")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.kind")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.target")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.lastUsed")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((w) => (
                  <tr key={w.id} className={`border-b last:border-0 hover:bg-muted/30 ${w.revoked ? "opacity-50" : ""}`}>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <WebhookIcon className="h-4 w-4 text-muted-foreground shrink-0 mt-0.5" />
                        <div>
                          <div className="font-medium">{w.name}</div>
                          <code className="text-xs-plus text-muted-foreground font-mono">{w.secret_prefix}…</code>
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-1">
                        <code className="text-xs font-mono text-muted-foreground" title={w.id}>
                          {w.id.slice(0, 8)}…
                        </code>
                        <button
                          onClick={() => copyId(w.id)}
                          className="ml-1 rounded p-0.5 hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                          title={w.id}
                        >
                          {copiedId === w.id ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                        </button>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="secondary" className="text-xs">{t(`kind.${w.kind}`)}</Badge>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {w.kind === "llm" && w.agent_id && (
                        <span className="inline-flex items-center gap-1"><Bot className="h-3.5 w-3.5" />{agentName(w.agent_id)}</span>
                      )}
                      {w.kind === "message" && w.channel_id && (
                        <span className="inline-flex items-center gap-1"><Radio className="h-3.5 w-3.5" />{channelName(w.channel_id)}</span>
                      )}
                      {!w.agent_id && !w.channel_id && "—"}
                    </td>
                    <td className="px-4 py-3">
                      {w.revoked ? (
                        <Badge variant="destructive" className="text-xs">{t("status.revoked")}</Badge>
                      ) : (
                        <Badge variant="default" className="text-xs">{t("status.active")}</Badge>
                      )}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground" title={w.last_used_at ? new Date(w.last_used_at).toLocaleString() : undefined}>
                      {w.last_used_at ? formatRelativeTime(w.last_used_at) : t("neverUsed")}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        {!w.revoked && (w.kind === "llm" || w.kind === "message") && (
                          <Button variant="ghost" size="sm" onClick={() => setTestTarget(w)} title={t("actions.test")}>
                            <Play className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        <Button variant="ghost" size="sm" onClick={() => setCallsTarget(w)} title={t("actions.calls")}>
                          <History className="h-3.5 w-3.5" />
                        </Button>
                        {!w.revoked && (
                          <>
                            <Button variant="ghost" size="sm" onClick={() => openEdit(w)} title={t("actions.edit")}>
                              <Pencil className="h-3.5 w-3.5" />
                            </Button>
                            <Button variant="ghost" size="sm" onClick={() => setRotateTarget(w)} title={t("actions.rotate")}>
                              <KeyRound className="h-3.5 w-3.5" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setRevokeTarget(w)}
                              className="text-destructive hover:text-destructive"
                              title={t("actions.revoke")}
                            >
                              <Ban className="h-3.5 w-3.5" />
                            </Button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
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
