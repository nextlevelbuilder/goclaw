import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { useMemorySearch } from "./hooks/use-memory";

interface MemorySearchDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
}

export function MemorySearchDialog({ open, onOpenChange, agentId }: MemorySearchDialogProps) {
  const { t } = useTranslation("memory");
  const [query, setQuery] = useState("");
  const [userId, setUserId] = useState("");
  const { results, searching, search } = useMemorySearch(agentId);

  const handleSearch = async () => {
    if (!query.trim()) return;
    await search(query.trim(), userId.trim() || undefined);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !searching) {
      handleSearch();
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent aria-describedby={undefined} className="max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("searchDialog.title")}</DialogTitle>
        </DialogHeader>

        <div className="flex gap-2">
          <div className="flex-1">
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t("searchDialog.queryPlaceholder")}
              autoFocus
            />
          </div>
          <div className="w-40">
            <Input
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
              placeholder={t("searchDialog.agentIdPlaceholder")}
            />
          </div>
          <Button onClick={handleSearch} disabled={searching || !query.trim()} className="gap-1">
            <Search className="h-3.5 w-3.5" />
            {searching ? "..." : t("searchDialog.search")}
          </Button>
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto mt-2 -mx-4 px-4 sm:-mx-6 sm:px-6">
          {results.length === 0 ? (
            <div className="py-12 text-center text-muted-foreground text-sm">
              {searching ? t("searchDialog.searching") : t("searchDialog.noResults")}
            </div>
          ) : (
            <div className="space-y-2">
              <p className="text-xs text-muted-foreground">{t("searchDialog.results")}: {results.length}</p>
              {results.map((r, i) => (
                <div key={i} className="rounded-md border p-3">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-mono text-xs font-medium">{r.path}</span>
                    <span className="text-xs text-muted-foreground">
                      L{r.start_line}-{r.end_line}
                    </span>
                    <ScoreBar score={r.score} />
                    {r.scope && (
                      <Badge variant={r.scope === "personal" ? "secondary" : "outline"} className="text-[10px]">
                        {r.scope}
                      </Badge>
                    )}
                  </div>
                  <pre className="text-xs text-muted-foreground whitespace-pre-wrap break-words">
                    {r.snippet}
                  </pre>
                </div>
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ScoreBar({ score }: { score: number }) {
  const pct = Math.min(100, Math.round(score * 100));
  return (
    <div className="flex items-center gap-1">
      <div className="h-1.5 w-12 rounded-full bg-muted overflow-hidden">
        <div
          className="h-full rounded-full bg-primary"
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="text-[10px] text-muted-foreground">{pct}%</span>
    </div>
  );
}
