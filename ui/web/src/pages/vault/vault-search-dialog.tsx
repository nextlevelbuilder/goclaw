import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { toast } from "@/stores/use-toast-store";
import { useVaultSearch } from "./hooks/use-vault";
import type { VaultDocument, VaultSearchResult } from "@/types/vault";

interface Props {
  agentId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSelectResult: (doc: VaultDocument) => void;
}

export function VaultSearchDialog({ agentId, open, onOpenChange, onSelectResult }: Props) {
  const { t } = useTranslation("vault");
  const { search } = useVaultSearch(agentId);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<VaultSearchResult[]>([]);
  const [searching, setSearching] = useState(false);

  const handleSearch = useCallback(async () => {
    if (!query.trim()) return;
    setSearching(true);
    try {
      const res = await search(query.trim());
      setResults(res);
    } catch {
      toast.error(t("searchFailed"));
    } finally {
      setSearching(false);
    }
  }, [query, search]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg max-sm:inset-0">
        <DialogHeader>
          <DialogTitle>{t("search")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-3">
          <div className="flex gap-2">
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("searchPlaceholder")}
              className="text-base md:text-sm"
              onKeyDown={(e) => e.key === "Enter" && handleSearch()}
            />
            <Button onClick={handleSearch} disabled={searching || !query.trim()} size="sm">
              <Search className="h-4 w-4" />
            </Button>
          </div>

          {searching && <div className="h-[100px] animate-pulse rounded-md bg-muted" />}

          {!searching && results.length === 0 && query && (
            <p className="text-xs text-muted-foreground text-center py-4">{t("noResults")}</p>
          )}

          {!searching && results.length > 0 && (
            <div className="space-y-1 max-h-[300px] overflow-y-auto">
              {results.map((r) => (
                <button
                  key={r.document.id}
                  className="w-full text-left p-2 rounded hover:bg-muted/50 flex items-center justify-between gap-2"
                  onClick={() => {
                    onSelectResult(r.document);
                    onOpenChange(false);
                  }}
                >
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{r.document.title || r.document.path}</p>
                    <p className="text-xs text-muted-foreground truncate">{r.document.path}</p>
                  </div>
                  <Badge variant="secondary" className="text-xs shrink-0">
                    {(r.score * 100).toFixed(0)}%
                  </Badge>
                </button>
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
