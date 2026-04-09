import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Link2, ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { formatRelativeTime } from "@/lib/format";
import type { VaultDocument } from "@/types/vault";

const DOC_DOT_COLORS: Record<string, string> = {
  context: "bg-blue-500",
  memory: "bg-purple-500",
  note: "bg-yellow-500",
  skill: "bg-green-500",
  episodic: "bg-orange-500",
  media: "bg-red-500",
};

interface Props {
  documents: VaultDocument[];
  selectedId: string | null;
  linkCounts: Map<string, number>;
  onSelect: (doc: VaultDocument) => void;
  loading: boolean;
  page: number;
  totalPages: number;
  total: number;
  onPageChange: (page: number) => void;
}

function DocCard({ doc, selected, linkCount, onClick }: {
  doc: VaultDocument; selected: boolean; linkCount: number; onClick: () => void;
}) {
  const { t } = useTranslation("vault");
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (selected) ref.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [selected]);

  return (
    <div
      ref={ref}
      className={`px-3 py-2 cursor-pointer border-b transition-colors ${selected ? "bg-accent/50" : "hover:bg-muted/50"}`}
      onClick={onClick}
    >
      <div className="flex items-center gap-2">
        <span className={`h-2 w-2 rounded-full shrink-0 ${DOC_DOT_COLORS[doc.doc_type] ?? "bg-gray-400"}`} />
        <span className="text-sm font-medium truncate flex-1">
          {doc.title || doc.path.split("/").pop()}
        </span>
        {linkCount > 0 && (
          <span className="flex items-center gap-0.5 text-2xs text-muted-foreground shrink-0">
            <Link2 className="h-3 w-3" />
            {linkCount}
          </span>
        )}
      </div>
      <div className="text-xs text-muted-foreground mt-0.5 pl-4">
        {t(`type.${doc.doc_type}`)} · {t(`scope.${doc.scope}`)}
      </div>
      <div className="text-2xs text-muted-foreground pl-4">
        {formatRelativeTime(doc.updated_at)}
      </div>
    </div>
  );
}

export function VaultDocumentSidebar({
  documents, selectedId, linkCounts, onSelect, loading, page, totalPages, total, onPageChange,
}: Props) {
  const { t } = useTranslation("vault");

  return (
    <div className="flex h-full flex-col border-r bg-background">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b">
        <span className="text-sm font-medium">{t("title")}</span>
        <span className="text-2xs text-muted-foreground">{total}</span>
      </div>

      {/* Doc list */}
      <div className="flex-1 overflow-y-auto">
        {loading && documents.length === 0 ? (
          Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="px-3 py-2 border-b space-y-1.5">
              <div className="h-4 w-3/4 animate-pulse rounded bg-muted" />
              <div className="h-3 w-1/2 animate-pulse rounded bg-muted" />
              <div className="h-2.5 w-1/3 animate-pulse rounded bg-muted" />
            </div>
          ))
        ) : documents.length === 0 ? (
          <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
            {t("noDocuments")}
          </div>
        ) : (
          documents.map((doc) => (
            <DocCard
              key={doc.id}
              doc={doc}
              selected={doc.id === selectedId}
              linkCount={linkCounts.get(doc.id) ?? 0}
              onClick={() => onSelect(doc)}
            />
          ))
        )}
      </div>

      {/* Pagination footer */}
      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-2 px-3 py-1.5 border-t text-xs text-muted-foreground">
          <Button variant="ghost" size="xs" disabled={page === 0} onClick={() => onPageChange(page - 1)}>
            <ChevronLeft className="h-3.5 w-3.5" />
          </Button>
          <span>{page + 1} / {totalPages}</span>
          <Button variant="ghost" size="xs" disabled={page >= totalPages - 1} onClick={() => onPageChange(page + 1)}>
            <ChevronRight className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}
    </div>
  );
}
