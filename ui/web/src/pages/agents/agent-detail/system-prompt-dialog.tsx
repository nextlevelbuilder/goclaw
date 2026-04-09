import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { useHttp } from "@/hooks/use-ws";
import { cn } from "@/lib/utils";

const MODES = ["full", "task", "minimal", "none"] as const;
type PromptMode = (typeof MODES)[number];

interface PreviewResponse {
  mode: string;
  prompt: string;
  token_count: number;
  sections: { name: string; start: number; end: number }[];
}

interface SystemPromptDialogProps {
  agentKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** Cache boundary marker — highlighted in the preview output. */
const CACHE_BOUNDARY = "<!-- GOCLAW_CACHE_BOUNDARY -->";

/** Replaces cache boundary HTML comment with a visible markdown separator. */
function insertCacheBoundary(prompt: string): string {
  return prompt.replace(
    CACHE_BOUNDARY,
    "\n\n---\n\n> **── cache boundary ──** stable above · dynamic below\n\n",
  );
}

export function SystemPromptDialog({ agentKey, open, onOpenChange }: SystemPromptDialogProps) {
  const { t } = useTranslation("agents");
  const http = useHttp();
  const [mode, setMode] = useState<PromptMode>("full");
  const [data, setData] = useState<PreviewResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const fetchPreview = useCallback(async (m: PromptMode) => {
    setLoading(true);
    setError("");
    try {
      const res = await http.get<PreviewResponse>(
        `/v1/agents/${agentKey}/system-prompt-preview?mode=${m}`,
      );
      setData(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load preview");
    } finally {
      setLoading(false);
    }
  }, [http, agentKey]);

  useEffect(() => {
    if (open) fetchPreview(mode);
  }, [mode, open, fetchPreview]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-5xl">
        <DialogHeader className="flex-row items-center justify-between gap-3">
          <DialogTitle>{t("files.systemPromptPreview")}</DialogTitle>
          {data && (
            <span className="rounded bg-muted px-2 py-0.5 text-xs tabular-nums text-muted-foreground">
              {data.token_count.toLocaleString()} {t("files.tokens")}
            </span>
          )}
        </DialogHeader>

        {/* Mode selector */}
        <div className="flex gap-1 border-b pb-2">
          {MODES.map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => setMode(m)}
              className={cn(
                "rounded-md px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer",
                mode === m
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-muted",
              )}
            >
              {m.charAt(0).toUpperCase() + m.slice(1)}
            </button>
          ))}
        </div>

        {/* Content — markdown rendered, keeps old content visible during loading */}
        <div className="relative flex-1 overflow-y-auto min-h-0">
          {loading && (
            <div className="absolute inset-0 z-10 flex items-start justify-center pt-16 bg-background/50">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          )}
          {error ? (
            <p className="text-sm text-destructive p-4">{error}</p>
          ) : data ? (
            <div className="prose prose-sm dark:prose-invert max-w-none p-1">
              <MarkdownRenderer content={insertCacheBoundary(data.prompt)} />
            </div>
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}
