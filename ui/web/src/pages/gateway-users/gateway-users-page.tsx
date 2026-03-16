import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Users, Trash2, Copy, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";
import { useGatewayUsers } from "./hooks/use-gateway-users";
import { GatewayUserCreateDialog } from "./gateway-user-create-dialog";
import type { GatewayUserData } from "@/types/gateway-user";

function formatDate(iso: string | null): string {
  if (!iso) return "\u2014";
  return new Date(iso).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

export function GatewayUsersPage() {
  const { t } = useTranslation("gateway-users");
  const { t: tc } = useTranslation("common");
  const { users, loading, refresh, createUser, deleteUser } = useGatewayUsers();

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && users.length === 0);
  const [search, setSearch] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<GatewayUserData | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [newToken, setNewToken] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [copiedHint, setCopiedHint] = useState<string | null>(null);

  const filtered = users.filter(
    (u) => u.user_id.toLowerCase().includes(search.toLowerCase()) || u.role.includes(search.toLowerCase()),
  );

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, resetPage]);

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteLoading(true);
    try {
      await deleteUser(deleteTarget.id);
      setDeleteTarget(null);
    } finally {
      setDeleteLoading(false);
    }
  };

  const handleCopy = async () => {
    if (!newToken) return;
    await navigator.clipboard.writeText(newToken);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button size="sm" onClick={() => setCreateOpen(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("addUser")}
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={spinning ? "animate-spin h-3.5 w-3.5" : "h-3.5 w-3.5"} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        <SearchInput value={search} onChange={setSearch} placeholder={t("searchPlaceholder")} className="max-w-sm" />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : filtered.length === 0 ? (
          <EmptyState icon={Users} title={t("emptyTitle")} description={t("emptyDescription")} />
        ) : (
          <>
            <div className="rounded-md border overflow-x-auto">
              <table className="w-full min-w-[600px] text-base md:text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="px-4 py-2 text-left font-medium">{t("columns.userId")}</th>
                    <th className="px-4 py-2 text-left font-medium">{t("columns.token")}</th>
                    <th className="px-4 py-2 text-left font-medium">{t("columns.role")}</th>
                    <th className="px-4 py-2 text-left font-medium">{t("columns.created")}</th>
                    <th className="px-4 py-2 text-right font-medium">{t("columns.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {pageItems.map((user) => (
                    <tr key={user.id} className="border-b last:border-0 hover:bg-muted/30">
                      <td className="px-4 py-2 font-medium">{user.user_id}</td>
                      <td className="px-4 py-2">
                        <div className="flex items-center gap-1.5">
                          <code className="rounded bg-muted px-1.5 py-0.5 text-xs">{user.token_hint || "****"}</code>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 w-6 p-0"
                            onClick={async () => {
                              await navigator.clipboard.writeText(user.gateway_token || "");
                              setCopiedHint(user.id);
                              setTimeout(() => setCopiedHint(null), 2000);
                            }}
                          >
                            {copiedHint === user.id ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                          </Button>
                        </div>
                      </td>
                      <td className="px-4 py-2">
                        <Badge variant={user.role === "root" ? "default" : "outline"}>
                          {t(`roles.${user.role}`)}
                        </Badge>
                      </td>
                      <td className="px-4 py-2 text-muted-foreground">{formatDate(user.created_at)}</td>
                      <td className="px-4 py-2 text-right">
                        {user.role !== "root" && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setDeleteTarget(user)}
                            className="gap-1 text-destructive hover:text-destructive"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pagination {...pagination} onPageChange={setPage} onPageSizeChange={setPageSize} />
          </>
        )}
      </div>

      <GatewayUserCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreate={async (input) => {
          const res = await createUser(input);
          setCreateOpen(false);
          setNewToken(res.gateway_token);
        }}
      />

      {/* Show-once token dialog */}
      <Dialog open={!!newToken} onOpenChange={(open) => !open && setNewToken(null)}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("created.title")}</DialogTitle>
            <DialogDescription>{t("created.description")}</DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <code className="flex-1 overflow-x-auto rounded bg-muted px-3 py-2 text-base md:text-sm font-mono break-all">
              {newToken}
            </code>
            <Button variant="outline" size="sm" onClick={handleCopy} className="gap-1 shrink-0">
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {copied ? t("created.copied") : t("created.copy")}
            </Button>
          </div>
          <DialogFooter>
            <Button onClick={() => setNewToken(null)}>{t("created.done")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={(v) => !v && setDeleteTarget(null)}
        title={t("delete.title")}
        description={t("delete.description", { name: deleteTarget?.user_id })}
        confirmValue={deleteTarget?.user_id || ""}
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={handleDelete}
        loading={deleteLoading}
      />
    </div>
  );
}
