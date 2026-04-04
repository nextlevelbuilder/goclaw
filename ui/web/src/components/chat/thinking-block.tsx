import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight, Brain } from "lucide-react";

interface ThinkingBlockProps {
  text: string;
  isStreaming?: boolean;
}

export function ThinkingBlock({ text, isStreaming = false }: ThinkingBlockProps) {
  const { t } = useTranslation("common");
  const [expanded, setExpanded] = useState(false);

  // Auto-expand when streaming starts, keep user's choice when done
  useEffect(() => {
    if (isStreaming) setExpanded(true);
  }, [isStreaming]);

  return (
    <div className="rounded-lg border border-muted bg-muted/50 text-sm overflow-hidden">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-3 py-2 text-muted-foreground hover:text-foreground transition-colors"
        onClick={() => setExpanded((v) => !v)}
      >
        <Brain className={`h-3.5 w-3.5 shrink-0 ${isStreaming ? "text-amber-500" : ""}`} />
        <span className="text-xs font-medium">
          {isStreaming ? t("thinkingStreaming") : t("thinking")}
        </span>
        {isStreaming && (
          <span className="inline-block w-1.5 h-3.5 bg-muted-foreground/50 animate-pulse rounded-sm" />
        )}
        <ChevronRight
          className={`ml-auto h-3 w-3 transition-transform ${expanded ? "rotate-90" : ""}`}
        />
      </button>
      {expanded && (
        <div className="border-t border-muted px-3 py-2">
          <pre className="whitespace-pre-wrap text-xs text-muted-foreground font-mono leading-relaxed max-h-80 overflow-y-auto break-words">
            {text}
            {isStreaming && (
              <span className="inline-block w-1.5 h-3.5 bg-muted-foreground/50 animate-pulse ml-0.5 align-text-bottom rounded-sm" />
            )}
          </pre>
        </div>
      )}
    </div>
  );
}
