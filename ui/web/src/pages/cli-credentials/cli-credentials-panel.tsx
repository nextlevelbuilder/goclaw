/**
 * CliCredentialsPanel — reusable panel without page-level PageHeader.
 * Used by:
 *  - CliCredentialsPage (standalone route, wraps in its own PageHeader)
 *  - CliCredentialsTab inside PackagesPage (tab body, no PageHeader needed)
 */
import { useState, lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { KeyRound, Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useCliCredentials, useCliCredentialPresets } from "./hooks/use-cli-credentials";
import { CliCredentialGrantsDialog } from "./cli-credential-grants-dialog";
import { CliCredentialsTable } from "./cli-credentials-table";
import type { SecureCLIBinary, CLICredentialInput } from "./hooks/use-cli-credentials";

const CliCredentialFormDialog = lazy(() =>
  import("./cli-credential-form-dialog").then((m) => ({ default: m.CliCredentialFormDialog }))
);
const CLIUserCredentialsDialog = lazy(() =>
  import("./cli-user-credentials-dialog").then((m) => ({ default: m.CLIUserCredentialsDialog }))
);

export function CliCredentialsPanel() {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");

  const [formOpen, setFormOpen] = useState(false);
  const [editItem, setEditItem] = useState<SecureCLIBinary | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SecureCLIBinary | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [userCredsTarget, setUserCredsTarget] = useState<SecureCLIBinary | null>(null);
  const [grantsTarget, setGrantsTarget] = useState<SecureCLIBinary | null>(null);

  const { items, loading, refresh, createCredential, updateCredential, deleteCredential } =
    useCliCredentials();
  const { presets } = useCliCredentialPresets();

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && items.length === 0);

  const handleCreate = async (data: CLICredentialInput) => { await createCredential(data); };
  const handleEdit = async (data: CLICredentialInput) => {
    if (!editItem) return;
    await updateCredential(editItem.id, data);
  };
  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await deleteCredential(deleteTarget.id);
      setDeleteTarget(null);
    } finally {
      setDeleteLoading(false);
    }
  };

  const openCreate = () => { setEditItem(null); setFormOpen(true); };
  const openEdit = (item: SecureCLIBinary) => { setEditItem(item); setFormOpen(true); };

  return (
    <div className="pb-10">
      {/* Toolbar */}
      <div className="flex items-center justify-between gap-2 mb-4">
        <p className="text-sm text-muted-foreground">{t("description")}</p>
        <div className="flex gap-2 shrink-0">
          <Button size="sm" onClick={openCreate} className="gap-1">
            <Plus className="h-3.5 w-3.5" /> {t("addCredential")}
          </Button>
          <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
          </Button>
        </div>
      </div>

      {showSkeleton ? (
        <TableSkeleton rows={5} />
      ) : items.length === 0 ? (
        <EmptyState icon={KeyRound} title={t("emptyTitle")} description={t("emptyDescription")} />
      ) : (
        <>
          <CliCredentialsTable
            items={items}
            onEdit={openEdit}
            onDelete={setDeleteTarget}
            onUserCreds={setUserCredsTarget}
            onGrants={setGrantsTarget}
          />
          {/* Finding #12: surface LIMIT 20 truncation so admins know there are more entries. */}
          {items.length >= 20 && (
            <p className="text-xs text-muted-foreground text-center pt-2">
              {t("list.truncated")}
            </p>
          )}
        </>
      )}

      <Suspense fallback={null}>
        <CliCredentialFormDialog
          open={formOpen}
          onOpenChange={setFormOpen}
          credential={editItem}
          presets={presets}
          onSubmit={editItem ? handleEdit : handleCreate}
        />
      </Suspense>

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { name: deleteTarget?.binary_name })}
        confirmLabel={t("delete.confirm")}
        variant="destructive"
        onConfirm={handleDelete}
        loading={deleteLoading}
      />

      {userCredsTarget && (
        <Suspense fallback={null}>
          <CLIUserCredentialsDialog
            open={!!userCredsTarget}
            onOpenChange={(open: boolean) => !open && setUserCredsTarget(null)}
            binary={userCredsTarget}
          />
        </Suspense>
      )}

      {grantsTarget && (
        <CliCredentialGrantsDialog
          open={!!grantsTarget}
          onOpenChange={(open) => !open && setGrantsTarget(null)}
          binary={grantsTarget}
        />
      )}
    </div>
  );
}
