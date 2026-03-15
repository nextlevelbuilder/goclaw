import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Plus, RefreshCw, Key, Ban, Copy, Check } from "lucide-react";
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
import { useApiKeys } from "./hooks/use-api-keys";
import { ApiKeyCreateDialog } from "./api-key-create-dialog";
import type { ApiKeyData } from "@/types/api-key";

function formatDate(iso: string | null): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

function keyStatus(key: ApiKeyData, t: (k: string) => string): { label: string; variant: "default" | "secondary" | "destructive" } {
  if (key.revoked) return { label: t("status.revoked"), variant: "destructive" };
  if (key.expires_at && new Date(key.expires_at) < new Date()) return { label: t("status.expired"), variant: "secondary" };
  return { label: t("status.active"), variant: "default" };
}

export function ApiKeysPage() {
  const { t } = useTranslation("api-keys");
  const { t: tc } = useTranslation("common");
  const { apiKeys, loading, refresh, createApiKey, revokeApiKey } = useApiKeys();

  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && apiKeys.length === 0);
  const [search, setSearch] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<ApiKeyData | null>(null);
  const [revokeLoading, setRevokeLoading] = useState(false);
  const [newKeyRaw, setNewKeyRaw] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const filtered = apiKeys.filter(
    (k) => k.name.toLowerCase().includes(search.toLowerCase()) || k.prefix.includes(search),
  );

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, resetPage]);

  const handleRevoke = async () => {
    if (!revokeTarget) return;
    setRevokeLoading(true);
    try {
      await revokeApiKey(revokeTarget.id);
      setRevokeTarget(null);
    } finally {
      setRevokeLoading(false);
    }
  };

  const handleCopy = async () => {
    if (!newKeyRaw) return;
    await navigator.clipboard.writeText(newKeyRaw);
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
              <Plus className="h-3.5 w-3.5" /> {t("addKey")}
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
          <EmptyState icon={Key} title={t("emptyTitle")} description={t("emptyDescription")} />
        ) : (
          <>
            <div className="rounded-md border overflow-x-auto">
              <table className="w-full min-w-[700px] text-base md:text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="px-4 py-2 text-left font-medium">{t("columns.name")}</th>
                    <th className="px-4 py-2 text-left font-medium">{t("columns.prefix")}</th>
                    <th className="px-4 py-2 text-left font-medium">{t("columns.scopes")}</th>
                    <th className="px-4 py-2 text-left font-medium">{t("columns.lastUsed")}</th>
                    <th className="px-4 py-2 text-left font-medium">{t("columns.created")}</th>
                    <th className="px-4 py-2 text-left font-medium">{t("columns.status")}</th>
                    <th className="px-4 py-2 text-right font-medium">{t("columns.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {pageItems.map((key) => {
                    const status = keyStatus(key, t);
                    return (
                      <tr key={key.id} className="border-b last:border-0 hover:bg-muted/30">
                        <td className="px-4 py-2 font-medium">{key.name}</td>
                        <td className="px-4 py-2">
                          <code className="rounded bg-muted px-1.5 py-0.5 text-xs">{key.prefix}...***</code>
                        </td>
                        <td className="px-4 py-2">
                          <div className="flex flex-wrap gap-1">
                            {key.scopes.map((s) => (
                              <Badge key={s} variant="outline" className="text-xs">
                                {s.replace("operator.", "")}
                              </Badge>
                            ))}
                          </div>
                        </td>
                        <td className="px-4 py-2 text-muted-foreground">{formatDate(key.last_used_at)}</td>
                        <td className="px-4 py-2 text-muted-foreground">{formatDate(key.created_at)}</td>
                        <td className="px-4 py-2">
                          <Badge variant={status.variant}>{status.label}</Badge>
                        </td>
                        <td className="px-4 py-2 text-right">
                          {!key.revoked && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setRevokeTarget(key)}
                              className="gap-1 text-destructive hover:text-destructive"
                            >
                              <Ban className="h-3.5 w-3.5" />
                            </Button>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <Pagination {...pagination} onPageChange={setPage} onPageSizeChange={setPageSize} />
          </>
        )}
      </div>

      <ApiKeyCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreate={async (input) => {
          const res = await createApiKey(input);
          setCreateOpen(false);
          setNewKeyRaw(res.key);
        }}
      />

      {/* Show-once key dialog */}
      <Dialog open={!!newKeyRaw} onOpenChange={(open) => !open && setNewKeyRaw(null)}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("created.title")}</DialogTitle>
            <DialogDescription>{t("created.description")}</DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <code className="flex-1 overflow-x-auto rounded bg-muted px-3 py-2 text-base md:text-sm font-mono break-all">
              {newKeyRaw}
            </code>
            <Button variant="outline" size="sm" onClick={handleCopy} className="gap-1 shrink-0">
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {copied ? t("created.copied") : t("created.copy")}
            </Button>
          </div>
          <DialogFooter>
            <Button onClick={() => setNewKeyRaw(null)}>{t("created.done")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDeleteDialog
        open={!!revokeTarget}
        onOpenChange={(v) => !v && setRevokeTarget(null)}
        title={t("revoke.title")}
        description={t("revoke.description", { name: revokeTarget?.name })}
        confirmValue={revokeTarget?.name || ""}
        confirmLabel={t("revoke.confirmLabel")}
        onConfirm={handleRevoke}
        loading={revokeLoading}
      />
    </div>
  );
}
