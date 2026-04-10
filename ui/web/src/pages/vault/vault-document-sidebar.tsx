import { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Link2, ChevronLeft, ChevronRight, Search, X, Loader2,
  FileText, Brain, StickyNote, Sparkles, Clock, Image,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatRelativeTime } from "@/lib/format";
import type { VaultDocument, VaultSearchResult } from "@/types/vault";
import { useVaultSearchAll } from "./hooks/use-vault";

const DOC_TYPE_CONFIG: Record<string, { color: string; bg: string; icon: typeof FileText; dotColor: string }> = {
  context:  { color: "text-blue-600 dark:text-blue-400",    bg: "bg-blue-500/10",    icon: FileText, dotColor: "bg-blue-500" },
  memory:   { color: "text-purple-600 dark:text-purple-400", bg: "bg-purple-500/10",  icon: Brain,    dotColor: "bg-purple-500" },
  note:     { color: "text-amber-600 dark:text-amber-400",   bg: "bg-amber-500/10",   icon: StickyNote, dotColor: "bg-amber-500" },
  skill:    { color: "text-emerald-600 dark:text-emerald-400", bg: "bg-emerald-500/10", icon: Sparkles, dotColor: "bg-emerald-500" },
  episodic: { color: "text-orange-600 dark:text-orange-400", bg: "bg-orange-500/10",  icon: Clock,    dotColor: "bg-orange-500" },
  media:    { color: "text-rose-600 dark:text-rose-400",     bg: "bg-rose-500/10",    icon: Image,    dotColor: "bg-rose-500" },
};

const DEFAULT_CONFIG = { color: "text-muted-foreground", bg: "bg-muted", icon: FileText, dotColor: "bg-muted-foreground" };
const DOC_TYPES = ["context", "memory", "note", "skill", "episodic", "media"] as const;

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
  docType: string;
  onDocTypeChange: (type: string) => void;
  agentId: string;
  teamId: string;
}

function DocCard({ doc, selected, linkCount, onClick }: {
  doc: VaultDocument; selected: boolean; linkCount: number; onClick: () => void;
}) {
  const { t } = useTranslation("vault");
  const ref = useRef<HTMLDivElement>(null);
  const cfg = DOC_TYPE_CONFIG[doc.doc_type] ?? DEFAULT_CONFIG;
  const Icon = cfg.icon;

  useEffect(() => {
    if (selected) ref.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [selected]);

  return (
    <div
      ref={ref}
      className={`group mx-1.5 my-0.5 flex items-center gap-2 rounded-md px-2 py-1.5 cursor-pointer transition-all ${
        selected
          ? "bg-accent shadow-sm ring-1 ring-accent-foreground/10"
          : "hover:bg-muted/60"
      }`}
      onClick={onClick}
    >
      <div className={`flex h-6 w-6 shrink-0 items-center justify-center rounded ${cfg.bg}`}>
        <Icon className={`h-3 w-3 ${cfg.color}`} />
      </div>
      <div className="min-w-0 flex-1">
        <span className="block truncate text-xs font-medium leading-snug">
          {doc.title || doc.path.split("/").pop()}
        </span>
        <div className="mt-0.5 flex items-center gap-1.5 text-2xs text-muted-foreground">
          <span>{t(`type.${doc.doc_type}`)}</span>
          <span>·</span>
          <span>{t(`scope.${doc.scope}`)}</span>
          {linkCount > 0 && (
            <>
              <span>·</span>
              <span className="flex items-center gap-0.5">
                <Link2 className="h-2.5 w-2.5" />
                {linkCount}
              </span>
            </>
          )}
          <span>·</span>
          <span>{formatRelativeTime(doc.updated_at)}</span>
        </div>
      </div>
    </div>
  );
}

export function VaultDocumentSidebar({
  documents, selectedId, linkCounts, onSelect, loading,
  page, totalPages, total, onPageChange,
  docType, onDocTypeChange, agentId, teamId,
}: Props) {
  const { t } = useTranslation("vault");
  const [query, setQuery] = useState("");
  const [searchResults, setSearchResults] = useState<VaultSearchResult[] | null>(null);
  const [searching, setSearching] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(null);
  const { search } = useVaultSearchAll();

  const doSearch = useCallback(
    async (q: string) => {
      if (!q.trim()) {
        setSearchResults(null);
        return;
      }
      setSearching(true);
      try {
        const results = await search(q, {
          agentId: agentId || undefined,
          docTypes: docType ? [docType] : undefined,
          teamId: teamId || undefined,
          maxResults: 30,
        });
        setSearchResults(results);
      } catch {
        setSearchResults([]);
      } finally {
        setSearching(false);
      }
    },
    [search, agentId, docType, teamId],
  );

  const handleQueryChange = (value: string) => {
    setQuery(value);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (!value.trim()) {
      setSearchResults(null);
      return;
    }
    debounceRef.current = setTimeout(() => doSearch(value), 300);
  };

  const clearSearch = () => {
    setQuery("");
    setSearchResults(null);
    inputRef.current?.focus();
  };

  // Show search results or regular document list
  const isSearchMode = query.trim().length > 0;
  const displayDocs = isSearchMode && searchResults
    ? searchResults.map((r) => r.document)
    : documents;

  return (
    <div className="flex h-full flex-col border-r bg-background">
      {/* Filter bar: type chips + search */}
      <div className="shrink-0 border-b px-2 py-1.5 space-y-1.5">
        {/* Type filter chips — horizontal scroll */}
        <div className="flex items-center gap-1 overflow-x-auto scrollbar-none">
          <button
            onClick={() => onDocTypeChange("")}
            className={`shrink-0 px-1.5 py-0.5 rounded text-2xs font-medium transition-colors ${
              !docType ? "bg-primary text-primary-foreground" : "bg-muted hover:bg-muted/80 text-muted-foreground"
            }`}
          >
            {t("allTypes")}
          </button>
          {DOC_TYPES.map((dt) => {
            const cfg = DOC_TYPE_CONFIG[dt] ?? DEFAULT_CONFIG;
            const active = docType === dt;
            return (
              <button
                key={dt}
                onClick={() => onDocTypeChange(active ? "" : dt)}
                className={`shrink-0 flex items-center gap-1 px-1.5 py-0.5 rounded text-2xs font-medium transition-colors ${
                  active ? "bg-primary text-primary-foreground" : "bg-muted hover:bg-muted/80 text-muted-foreground"
                }`}
              >
                <span className={`h-1.5 w-1.5 rounded-full ${active ? "bg-primary-foreground" : cfg.dotColor}`} />
                {t(`type.${dt}`)}
              </button>
            );
          })}
          <Badge variant="secondary" className="text-2xs tabular-nums ml-auto shrink-0">{total}</Badge>
        </div>

        {/* Search input */}
        <div className="relative">
          {searching
            ? <Loader2 className="absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground animate-spin" />
            : <Search className="absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground" />
          }
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => handleQueryChange(e.target.value)}
            placeholder={t("searchPlaceholder")}
            className="w-full rounded-md border bg-background pl-7 pr-7 py-1 text-base md:text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          />
          {query && (
            <button
              onClick={clearSearch}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
      </div>

      {/* Doc list */}
      <div className="flex-1 overflow-y-auto py-1">
        {(loading || searching) && displayDocs.length === 0 ? (
          Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="mx-1.5 my-0.5 flex items-center gap-2 rounded-md px-2 py-1.5">
              <div className="h-6 w-6 shrink-0 animate-pulse rounded bg-muted" />
              <div className="flex-1 space-y-1">
                <div className="h-3.5 w-3/4 animate-pulse rounded bg-muted" />
                <div className="h-2.5 w-1/2 animate-pulse rounded bg-muted" />
              </div>
            </div>
          ))
        ) : displayDocs.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-32 gap-1 text-muted-foreground">
            <FileText className="h-5 w-5" />
            <span className="text-sm">{isSearchMode ? t("noResults") : t("noDocuments")}</span>
          </div>
        ) : (
          displayDocs.map((doc) => (
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

      {/* Pagination footer — hidden during search */}
      {!isSearchMode && totalPages > 1 && (
        <div className="flex items-center justify-center gap-2 px-3 py-1.5 border-t text-xs text-muted-foreground">
          <Button variant="ghost" size="xs" disabled={page === 0} onClick={() => onPageChange(page - 1)}>
            <ChevronLeft className="h-3.5 w-3.5" />
          </Button>
          <span className="tabular-nums">{page + 1} / {totalPages}</span>
          <Button variant="ghost" size="xs" disabled={page >= totalPages - 1} onClick={() => onPageChange(page + 1)}>
            <ChevronRight className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}
    </div>
  );
}
