import { Brain, RotateCw, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Pagination } from "@/components/shared/pagination";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { formatUserLabel } from "@/lib/format-user-label";
import { useTranslation } from "react-i18next";
import type { MemoryDocument } from "@/types/memory";

type ContactResolver = (id: string) => { display_name?: string; username?: string } | null;

interface MemoryDocumentsTableProps {
  documents: MemoryDocument[];
  paginatedDocs: MemoryDocument[];
  loading: boolean;
  agentId: string;
  agentWorkspace?: string;
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
  resolveContact: ContactResolver;
  agentMap: Map<string, string>;
  onViewDoc: (doc: MemoryDocument) => void;
  onDeleteTarget: (doc: MemoryDocument) => void;
  onReindex: (doc: MemoryDocument) => void;
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
}

export function MemoryDocumentsTable({
  documents,
  paginatedDocs,
  loading,
  agentId,
  agentWorkspace,
  page,
  pageSize,
  total,
  totalPages,
  resolveContact,
  agentMap,
  onViewDoc,
  onDeleteTarget,
  onReindex,
  onPageChange,
  onPageSizeChange,
}: MemoryDocumentsTableProps) {
  const { t } = useTranslation("memory");

  if (loading && documents.length === 0) {
    return <TableSkeleton rows={5} />;
  }

  if (documents.length === 0) {
    return (
      <EmptyState
        icon={Brain}
        title={t("emptyTitle")}
        description={agentId ? t("emptyAgentDescription") : t("emptyGlobalDescription")}
      />
    );
  }

  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full min-w-[600px] text-sm">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="px-4 py-3 text-left font-medium">{t("columns.path")}</th>
            {!agentId && <th className="px-4 py-3 text-left font-medium">{t("columns.agent")}</th>}
            <th className="px-4 py-3 text-left font-medium">{t("columns.scope")}</th>
            <th className="px-4 py-3 text-left font-medium">{t("columns.hash")}</th>
            <th className="px-4 py-3 text-left font-medium">{t("columns.updated")}</th>
            <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
          </tr>
        </thead>
        <tbody>
          {paginatedDocs.map((doc) => (
            <tr key={`${doc.agent_id}-${doc.path}-${doc.user_id || "global"}`} className="border-b last:border-0 hover:bg-muted/30">
              <td className="px-4 py-3">
                <button
                  className="flex items-start gap-2 text-left hover:underline cursor-pointer"
                  onClick={() => onViewDoc(doc)}
                >
                  <Brain className="h-4 w-4 shrink-0 text-muted-foreground mt-0.5" />
                  <div>
                    <span className="font-mono text-xs font-medium">{doc.path}</span>
                    {agentWorkspace && (
                      <p className="font-mono text-[10px] text-muted-foreground">{agentWorkspace}</p>
                    )}
                  </div>
                </button>
              </td>
              {!agentId && (
                <td className="px-4 py-3 text-xs text-muted-foreground">
                  {doc.agent_id ? (agentMap.get(doc.agent_id) || doc.agent_id.slice(0, 8)) : "-"}
                </td>
              )}
              <td className="px-4 py-3">
                <Badge variant={doc.user_id ? "secondary" : "outline"}>
                  {doc.user_id ? t("scopeLabel.personal") : t("scopeLabel.global")}
                </Badge>
                {doc.user_id && (
                  <span className="ml-1 text-xs text-muted-foreground">{formatUserLabel(doc.user_id, resolveContact)}</span>
                )}
              </td>
              <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                {doc.hash.slice(0, 8)}
              </td>
              <td className="px-4 py-3 text-xs text-muted-foreground">
                {new Date(doc.updated_at).toLocaleString()}
              </td>
              <td className="px-4 py-3 text-right">
                <div className="flex items-center justify-end gap-1">
                  <Button variant="ghost" size="sm" onClick={() => onReindex(doc)} className="gap-1">
                    <RotateCw className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => onDeleteTarget(doc)}
                    className="gap-1 text-destructive hover:text-destructive"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <Pagination
        page={page}
        pageSize={pageSize}
        total={total}
        totalPages={totalPages}
        onPageChange={onPageChange}
        onPageSizeChange={onPageSizeChange}
      />
    </div>
  );
}
