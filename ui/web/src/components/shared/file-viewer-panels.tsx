/**
 * Internal viewer panels used by FileContentBody:
 * - CodeViewer: syntax-highlighted code with copy button
 * - CsvViewer: tabular CSV display with copy button
 * - ImageViewer: blob-fetched image display
 * - UnsupportedFileViewer: fallback with download button
 */
import { useMemo, useRef, useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Check, Copy, Download, FileQuestion, Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useClipboard } from "@/hooks/use-clipboard";
import { formatSize, sizeBadgeVariant } from "@/lib/file-helpers";
import hljs from "highlight.js/lib/core";
import typescript from "highlight.js/lib/languages/typescript";
import javascript from "highlight.js/lib/languages/javascript";
import python from "highlight.js/lib/languages/python";
import go from "highlight.js/lib/languages/go";
import bash from "highlight.js/lib/languages/bash";
import json from "highlight.js/lib/languages/json";
import yaml from "highlight.js/lib/languages/yaml";
import css from "highlight.js/lib/languages/css";
import xml from "highlight.js/lib/languages/xml";
import sql from "highlight.js/lib/languages/sql";
import rust from "highlight.js/lib/languages/rust";
import ruby from "highlight.js/lib/languages/ruby";
import java from "highlight.js/lib/languages/java";
import c from "highlight.js/lib/languages/c";
import cpp from "highlight.js/lib/languages/cpp";
import lua from "highlight.js/lib/languages/lua";

hljs.registerLanguage("typescript", typescript);
hljs.registerLanguage("tsx", typescript);
hljs.registerLanguage("javascript", javascript);
hljs.registerLanguage("jsx", javascript);
hljs.registerLanguage("python", python);
hljs.registerLanguage("go", go);
hljs.registerLanguage("bash", bash);
hljs.registerLanguage("json", json);
hljs.registerLanguage("yaml", yaml);
hljs.registerLanguage("css", css);
hljs.registerLanguage("html", xml);
hljs.registerLanguage("xml", xml);
hljs.registerLanguage("sql", sql);
hljs.registerLanguage("rust", rust);
hljs.registerLanguage("ruby", ruby);
hljs.registerLanguage("java", java);
hljs.registerLanguage("c", c);
hljs.registerLanguage("cpp", cpp);
hljs.registerLanguage("lua", lua);

export function CodeViewer({ content, language }: { content: string; language: string }) {
  const { copied, copy } = useClipboard();
  const { t } = useTranslation("common");
  const codeRef = useRef<HTMLElement>(null);

  const highlighted = useMemo(() => {
    if (language && hljs.getLanguage(language)) {
      try {
        return hljs.highlight(content, { language }).value;
      } catch { /* fallback */ }
    }
    return null;
  }, [content, language]);

  return (
    <div className="group relative overflow-hidden rounded-lg border border-border/60">
      <div className="flex items-center justify-between border-b border-border/40 bg-muted/70 px-3 py-1.5 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
        <span>{language || "text"}</span>
        <button
          type="button"
          onClick={() => copy(content)}
          className="cursor-pointer opacity-0 transition-opacity group-hover:opacity-100"
          title={t("copy")}
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>
      <pre className="overflow-auto bg-muted/30 p-4 text-[13px] leading-relaxed hljs">
        {highlighted ? (
          <code
            ref={codeRef}
            style={{ fontFamily: "'JetBrains Mono', 'Fira Code', ui-monospace, monospace" }}
            dangerouslySetInnerHTML={{ __html: highlighted }}
          />
        ) : (
          <code style={{ fontFamily: "'JetBrains Mono', 'Fira Code', ui-monospace, monospace" }}>
            {content}
          </code>
        )}
      </pre>
    </div>
  );
}

export function CsvViewer({ content }: { content: string }) {
  const { copied, copy } = useClipboard();
  const { t } = useTranslation("common");
  const rows = useMemo(() => {
    return content.split("\n").filter(Boolean).map((line) => {
      const cols: string[] = [];
      let cur = "";
      let inQuote = false;
      for (let i = 0; i < line.length; i++) {
        const ch = line[i];
        if (ch === '"') { inQuote = !inQuote; continue; }
        if (ch === "," && !inQuote) { cols.push(cur.trim()); cur = ""; continue; }
        cur += ch;
      }
      cols.push(cur.trim());
      return cols;
    });
  }, [content]);

  const header = rows[0];
  if (!header || rows.length === 0) return <pre className="text-sm p-4">{content}</pre>;
  const body = rows.slice(1);

  return (
    <div className="group relative rounded-lg border border-border/60 flex flex-col overflow-hidden">
      <div className="flex items-center justify-between border-b border-border/40 bg-muted/70 px-3 py-1.5 text-[11px] font-medium tracking-wide text-muted-foreground uppercase shrink-0">
        <span>{t("csvRows", { count: body.length })}</span>
        <button
          type="button"
          onClick={() => copy(content)}
          className="cursor-pointer opacity-0 transition-opacity group-hover:opacity-100"
          title={t("copy")}
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>
      <div className="overflow-auto flex-1 min-h-0">
        <table className="w-full text-[13px] border-collapse">
          <thead className="sticky top-0 z-10">
            <tr className="bg-muted/70">
              {header.map((col, i) => (
                <th key={i} className="px-3 py-2 text-left text-xs font-semibold tracking-wide border-b border-border/60 whitespace-nowrap">
                  {col}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {body.map((row, i) => (
              <tr key={i} className="border-b border-border/40 last:border-0 even:bg-muted/30 hover:bg-muted/50">
                {header.map((_, j) => (
                  <td key={j} className="px-3 py-1.5 border-r border-border/30 last:border-r-0">
                    {row[j] ?? ""}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export function ImageViewer({ path, fetchBlob }: { path: string; fetchBlob: (path: string) => Promise<Blob> }) {
  const { t } = useTranslation("common");
  const [src, setSrc] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  useEffect(() => {
    let objectUrl: string | null = null;
    setLoading(true);
    setError(false);

    fetchBlob(path)
      .then((blob) => {
        objectUrl = URL.createObjectURL(blob);
        setSrc(objectUrl);
      })
      .catch(() => setError(true))
      .finally(() => setLoading(false));

    return () => {
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [path, fetchBlob]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error || !src) {
    return (
      <div className="flex items-center justify-center py-12 text-sm text-muted-foreground">
        {t("failedToLoadImage")}
      </div>
    );
  }

  return (
    <div className="flex items-center justify-center p-4">
      <img
        src={src}
        alt={path.split("/").pop() ?? ""}
        className="max-w-full max-h-[70vh] object-contain rounded-lg border border-border/40"
      />
    </div>
  );
}

export function UnsupportedFileViewer({
  path,
  size,
  onDownload,
}: {
  path: string;
  size: number;
  onDownload?: () => void;
}) {
  const { t } = useTranslation("storage");
  const fileName = path.split("/").pop() ?? path;

  return (
    <div className="flex flex-col items-center justify-center py-16 gap-4">
      <FileQuestion className="h-12 w-12 text-muted-foreground/50" />
      <p className="text-sm text-muted-foreground">{t("unsupportedFile")}</p>
      {onDownload && (
        <Button variant="outline" size="sm" onClick={onDownload}>
          <Download className="h-3.5 w-3.5 mr-1.5" />
          {fileName}
          <Badge variant={sizeBadgeVariant(size)} className="text-[10px] ml-1.5">
            {formatSize(size)}
          </Badge>
        </Button>
      )}
    </div>
  );
}
