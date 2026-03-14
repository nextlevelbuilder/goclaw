import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Cpu, Plus, RefreshCw, Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useProviders, type ProviderData, type ProviderInput } from "./hooks/use-providers";
import { ProviderFormDialog } from "./provider-form-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { usePagination } from "@/hooks/use-pagination";

const typeBadge: Record<string, { label: string; variant: "default" | "secondary" | "outline" }> = {
  anthropic_native: { label: "Anthropic", variant: "default" },
  openai_compat: { label: "OpenAI Compat", variant: "secondary" },
  gemini_native: { label: "Gemini", variant: "secondary" },
  openrouter: { label: "OpenRouter", variant: "secondary" },
  groq: { label: "Groq", variant: "secondary" },
  deepseek: { label: "DeepSeek", variant: "secondary" },
  mistral: { label: "Mistral", variant: "secondary" },
  xai: { label: "xAI", variant: "secondary" },
  minimax_native: { label: "MiniMax", variant: "secondary" },
  cohere: { label: "Cohere", variant: "secondary" },
  perplexity: { label: "Perplexity", variant: "secondary" },
  chatgpt_oauth: { label: "ChatGPT (OAuth)", variant: "default" },
  yescale: { label: "YesScale", variant: "secondary" },
  acp: { label: "ACP", variant: "outline" },
};

export function ProvidersPage() {
  const { t } = useTranslation("providers");
  const { t: tc } = useTranslation("common");
  const {
    providers, loading, refresh,
    createProvider, updateProvider, deleteProvider,
  } = useProviders();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && providers.length === 0);
  const [search, setSearch] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editProvider, setEditProvider] = useState<ProviderData | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ProviderData | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  const filtered = providers.filter(
    (p) =>
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      (p.display_name || "").toLowerCase().includes(search.toLowerCase()),
  );

  const { pageItems, pagination, setPage, setPageSize, resetPage } = usePagination(filtered);

  useEffect(() => { resetPage(); }, [search, resetPage]);

  const handleCreate = async (data: ProviderInput) => {
    await createProvider(data);
  };

  const handleEdit = async (data: ProviderInput) => {
    if (!editProvider) return;
    await updateProvider(editProvider.id, data);
  };

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
    <div className="p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button size="sm" onClick={() => { setEditProvider(null); setFormOpen(true); }} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("addProvider")}
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={5} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Cpu}
            title={search ? t("noMatchTitle") : t("emptyTitle")}
            description={search ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[700px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.type")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.apiBase")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.apiKey")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {pageItems.map((p) => {
                  const tb = typeBadge[p.provider_type] ?? { label: p.provider_type, variant: "outline" as const };
                  return (
                    <tr key={p.id} className="border-b last:border-0 hover:bg-muted/30">
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <Cpu className="h-4 w-4 text-muted-foreground" />
                          <div>
                            <span className="font-medium">{p.display_name || p.name}</span>
                            {p.display_name && (
                              <span className="ml-1 text-xs text-muted-foreground">({p.name})</span>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <Badge variant={tb.variant}>{tb.label}</Badge>
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-muted-foreground max-w-[200px] truncate">
                        {p.api_base || "-"}
                      </td>
                      <td className="px-4 py-3">
                        {p.provider_type === "chatgpt_oauth" ? (
                          <Badge variant="outline" className="text-xs">{t("apiKey.oauthToken")}</Badge>
                        ) : p.api_key === "***" ? (
                          <Badge variant="outline" className="font-mono text-xs">***</Badge>
                        ) : (
                          <span className="text-xs text-muted-foreground">{t("apiKey.notSet")}</span>
                        )}
                      </td>
                      <td className="px-4 py-3">
                        <Badge variant={p.enabled ? "default" : "secondary"}>
                          {p.enabled ? tc("enabled") : tc("disabled")}
                        </Badge>
                      </td>
                      <td className="px-4 py-3 text-right">
                        {p.provider_type !== "chatgpt_oauth" && (
                          <div className="flex items-center justify-end gap-1">
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => { setEditProvider(p); setFormOpen(true); }}
                            >
                              <Pencil className="h-3.5 w-3.5" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setDeleteTarget(p)}
                              className="text-destructive hover:text-destructive"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <Pagination
              page={pagination.page}
              pageSize={pagination.pageSize}
              total={pagination.total}
              totalPages={pagination.totalPages}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
          </div>
        )}
      </div>

      <ProviderFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        provider={editProvider}
        onSubmit={editProvider ? handleEdit : handleCreate}
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
