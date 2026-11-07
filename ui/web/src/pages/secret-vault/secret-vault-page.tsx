import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, ShieldCheck, Pencil, Users, ScrollText, Trash2, CheckCircle2, Bot } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { formatRelativeTime } from "@/lib/format";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useSecretVault } from "./hooks/use-secret-vault";
import { SecretVaultFormDialog } from "./secret-vault-form-dialog";
import { SecretVaultGrantsDialog } from "./secret-vault-grants-dialog";
import { SecretVaultAuditDialog } from "./secret-vault-audit-dialog";
import { SecretVaultApproveDialog } from "./secret-vault-approve-dialog";
import { SecretVaultAgentWriteDialog } from "./secret-vault-agent-write-dialog";
import { isPending } from "./secret-vault-utils";
import type { SecretVaultEntry } from "@/types/secret-vault";

export function SecretVaultPage() {
  const { t } = useTranslation("secret-vault");
  const { t: tc } = useTranslation("common");
  const { items, loading, refresh, createSecret, updateMeta, replaceValue, deleteSecret, approveSecret } =
    useSecretVault();
  const { agents } = useAgents();

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && items.length === 0);
  const [search, setSearch] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<SecretVaultEntry | null>(null);
  const [grantsFor, setGrantsFor] = useState<SecretVaultEntry | null>(null);
  const [auditFor, setAuditFor] = useState<SecretVaultEntry | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SecretVaultEntry | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [pendingOnly, setPendingOnly] = useState(false);
  const [approveFor, setApproveFor] = useState<SecretVaultEntry | null>(null);
  const [agentWriteOpen, setAgentWriteOpen] = useState(false);

  const pendingCount = items.filter(isPending).length;
  const filtered = items.filter(
    (e) => (!pendingOnly || isPending(e)) && e.name.toLowerCase().includes(search.toLowerCase()),
  );

  const agentName = (id: string | null): string | null => {
    if (!id) return null;
    const a = agents.find((x) => x.id === id);
    return a ? a.display_name || a.agent_key : null;
  };

  const openCreate = () => {
    setEditing(null);
    setFormOpen(true);
  };
  const openEdit = (e: SecretVaultEntry) => {
    setEditing(e);
    setFormOpen(true);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await deleteSecret(deleteTarget.id);
      setDeleteTarget(null);
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex flex-wrap gap-2">
            <Button size="sm" onClick={openCreate} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("add")}
            </Button>
            <Button variant="outline" size="sm" onClick={() => setAgentWriteOpen(true)} className="gap-1">
              <Bot className="h-3.5 w-3.5" /> {t("agentWrite.open")}
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={spinning ? "animate-spin h-3.5 w-3.5" : "h-3.5 w-3.5"} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={search} onChange={setSearch} placeholder={t("searchPlaceholder")} className="max-w-sm" />
        <div className="flex gap-1" role="group">
          <Button
            variant={pendingOnly ? "outline" : "secondary"}
            size="sm"
            aria-pressed={!pendingOnly}
            onClick={() => setPendingOnly(false)}
          >
            {t("filter.all")}
          </Button>
          <Button
            variant={pendingOnly ? "secondary" : "outline"}
            size="sm"
            aria-pressed={pendingOnly}
            onClick={() => setPendingOnly(true)}
          >
            {t("filter.pending", { count: pendingCount })}
          </Button>
        </div>
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={4} />
        ) : filtered.length === 0 ? (
          <EmptyState icon={ShieldCheck} title={t("emptyTitle")} description={t("emptyDescription")} />
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.kind")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.domains")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.approval")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.updated")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((e) => (
                  <tr key={e.id} className="border-b last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="font-medium font-mono">{e.name}</span>
                        {isPending(e) && (
                          <Badge variant="warning" className="text-xs">{t("status.pending")}</Badge>
                        )}
                      </div>
                      {e.created_via === "agent" && (
                        <div className="text-xs text-muted-foreground">
                          {t("status.savedByAgent")}
                          {agentName(e.created_by_agent) ? `: ${agentName(e.created_by_agent)}` : ""}
                        </div>
                      )}
                      {e.hints && <div className="text-xs text-muted-foreground">{e.hints}</div>}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="secondary" className="text-xs">{t(`kind.${e.kind}`)}</Badge>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-1">
                        {e.allowed_domains.map((d) => (
                          <Badge key={d} variant="outline" className="font-mono text-xs-plus px-1.5 py-0">{d}</Badge>
                        ))}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={e.require_approval ? "default" : "secondary"} className="text-xs">
                        {e.require_approval ? t("approval.required") : t("approval.notRequired")}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">{formatRelativeTime(e.updated_at)}</td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        {isPending(e) && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setApproveFor(e)}
                            title={t("actions.approve")}
                            className="gap-1 text-success-text"
                          >
                            <CheckCircle2 className="h-3.5 w-3.5" />
                            <span className="hidden sm:inline">{t("actions.approve")}</span>
                          </Button>
                        )}
                        <Button variant="ghost" size="sm" onClick={() => openEdit(e)} title={t("actions.edit")}>
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setGrantsFor(e)} title={t("actions.access")}>
                          <Users className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setAuditFor(e)} title={t("actions.audit")}>
                          <ScrollText className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDeleteTarget(e)}
                          className="text-destructive hover:text-destructive"
                          title={t("actions.delete")}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <SecretVaultFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        entry={editing}
        onCreate={createSecret}
        onUpdate={async (id, meta, value) => {
          const current = items.find((e) => e.id === id);
          if (!current) return;
          // Each write returns the entry at its new revision; the value write
          // must be pinned to that one, not to the revision the form opened with.
          const updated = await updateMeta(id, meta, current.revision);
          if (value) await replaceValue(id, value, updated.revision);
        }}
      />
      <SecretVaultGrantsDialog entry={grantsFor} onClose={() => setGrantsFor(null)} />
      <SecretVaultAuditDialog entry={auditFor} onClose={() => setAuditFor(null)} />
      <SecretVaultApproveDialog
        key={approveFor?.id ?? "none"}
        entry={approveFor}
        agentName={agentName(approveFor?.created_by_agent ?? null)}
        onApprove={(id, domains) => approveSecret(id, { allowed_domains: domains })}
        onClose={() => setApproveFor(null)}
      />
      <SecretVaultAgentWriteDialog open={agentWriteOpen} onOpenChange={setAgentWriteOpen} />

      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { name: deleteTarget?.name })}
        confirmValue={deleteTarget?.name || ""}
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={handleDelete}
        loading={deleting}
      />
    </div>
  );
}
