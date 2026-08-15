import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Copy, Check } from "lucide-react";
import { useClipboard } from "@/hooks/use-clipboard";
import hljs from "highlight.js/lib/core";
import json from "highlight.js/lib/languages/json";

hljs.registerLanguage("json", json);

/** Try to pretty-print JSON; returns { text, isJson }. */
function formatPreview(text: string): { text: string; isJson: boolean } {
  const trimmed = text.trim();
  if ((trimmed.startsWith("{") && trimmed.endsWith("}")) || (trimmed.startsWith("[") && trimmed.endsWith("]"))) {
    try {
      return { text: JSON.stringify(JSON.parse(trimmed), null, 2), isJson: true };
    } catch { /* not valid JSON */ }
  }
  return { text, isJson: false };
}

export function TracePreviewBlock({ label, content }: { label: string; content: string }) {
  const { t } = useTranslation("traces");
  const { copied, copy } = useClipboard();
  const { text: formatted, isJson } = useMemo(() => formatPreview(content), [content]);
  const highlightedHtml = useMemo(() => {
    if (!isJson) return null;
    return hljs.highlight(formatted, { language: "json" }).value;
  }, [formatted, isJson]);

  return (
    <div className="relative rounded-md border p-3">
      <p className="mb-1 text-xs font-medium text-muted-foreground">{label}</p>
      <button
        type="button"
        onClick={() => copy(content)}
        className="absolute right-2 top-2 flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
      >
        {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
        {t("detail.copy")}
      </button>
      {highlightedHtml ? (
        <pre className="hljs mt-1 max-h-[20vh] overflow-y-auto whitespace-pre-wrap break-all text-xs sm:max-h-[40vh]" dangerouslySetInnerHTML={{ __html: highlightedHtml }} />
      ) : (
        <pre className="mt-1 max-h-[20vh] overflow-y-auto whitespace-pre-wrap break-all text-xs sm:max-h-[40vh]">{formatted}</pre>
      )}
    </div>
  );
}
