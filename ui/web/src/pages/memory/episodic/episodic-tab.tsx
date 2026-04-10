import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Search, Brain } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useEpisodicSummaries, useEpisodicSearch } from "../hooks/use-episodic";
import { formatRelativeTime } from "@/lib/format";
import type { EpisodicSummary, EpisodicSearchResult } from "@/types/memory";

const SOURCE_COLORS: Record<string, string> = {
  session: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  v2_daily: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
  manual: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
};

interface Props {
  agentId: string;
}

export function EpisodicTab({ agentId }: Props) {
  const { t } = useTranslation("memory");
  const { summaries, loading } = useEpisodicSummaries(agentId, { limit: 50 });
  const { search } = useEpisodicSearch(agentId);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<EpisodicSearchResult[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const handleSearch = useCallback(async () => {
    if (!searchQuery.trim()) { setSearchResults(null); return; }
    setSearching(true);
    const results = await search(searchQuery.trim());
    setSearchResults(results);
    setSearching(false);
  }, [searchQuery, search]);

  const toggleExpand = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };

  if (!agentId) {
    return <p className="text-xs text-muted-foreground py-8 text-center">{t("episodic.selectAgent")}</p>;
  }

  if (loading && summaries.length === 0) {
    return <div className="h-[200px] animate-pulse rounded-md bg-muted" />;
  }

  if (summaries.length === 0 && !searchResults) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center space-y-2">
        <Brain className="h-8 w-8 text-muted-foreground/40" />
        <p className="text-sm font-medium">{t("episodic.noData")}</p>
        <p className="text-xs text-muted-foreground">{t("episodic.noDataHint")}</p>
      </div>
    );
  }

  const renderSummary = (s: EpisodicSummary) => (
    <div key={s.id} className="border rounded-md p-3 space-y-2 hover:bg-muted/30">
      {/* L0 abstract */}
      <p className="text-sm font-medium">{s.l0_abstract}</p>
      {/* Badges */}
      <div className="flex flex-wrap gap-1.5 items-center">
        <Badge variant="outline" className={SOURCE_COLORS[s.source_type] ?? ""}>
          {t(`episodic.source.${s.source_type}`)}
        </Badge>
        {s.key_topics.map((topic) => (
          <Badge key={topic} variant="secondary" className="text-xs">{topic}</Badge>
        ))}
        <span className="text-xs text-muted-foreground ml-auto">
          {s.turn_count} {t("episodic.turns")} · {s.token_count} {t("episodic.tokens")} · {formatRelativeTime(s.created_at)}
        </span>
      </div>
      {/* Expandable summary */}
      <button
        onClick={() => toggleExpand(s.id)}
        className="text-xs text-primary hover:underline"
      >
        {expanded.has(s.id) ? t("episodic.collapse") : t("episodic.expand")}
      </button>
      {expanded.has(s.id) && (
        <p className="text-xs text-muted-foreground whitespace-pre-wrap border-t pt-2">{s.summary}</p>
      )}
    </div>
  );

  return (
    <div className="space-y-4">
      {/* Search bar */}
      <div className="flex gap-2">
        <Input
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder={t("episodic.searchPlaceholder")}
          className="text-base md:text-sm"
          onKeyDown={(e) => e.key === "Enter" && handleSearch()}
        />
        <Button size="sm" variant="outline" onClick={handleSearch} disabled={searching}>
          <Search className="h-4 w-4" />
        </Button>
        {searchResults && (
          <Button size="sm" variant="ghost" onClick={() => { setSearchResults(null); setSearchQuery(""); }}>
            {t("episodic.clearSearch")}
          </Button>
        )}
      </div>

      {/* Search results or full list */}
      {searching && <div className="h-[100px] animate-pulse rounded-md bg-muted" />}

      {searchResults && !searching && (
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">{searchResults.length} {t("episodic.results")}</p>
          {searchResults.map((r) => (
            <div key={r.episodic_id} className="border rounded-md p-3 space-y-1">
              <p className="text-sm">{r.l0_abstract}</p>
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Badge variant="secondary" className="text-xs">{(r.score * 100).toFixed(0)}%</Badge>
                <span>{formatRelativeTime(r.created_at)}</span>
                <span>{r.session_key}</span>
              </div>
            </div>
          ))}
        </div>
      )}

      {!searchResults && (
        <div className="space-y-2">
          {summaries.map(renderSummary)}
        </div>
      )}
    </div>
  );
}
