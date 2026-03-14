import { useTranslation } from "react-i18next";
import { X, FileCode2 } from "lucide-react";
import { extOf, langFor } from "@/lib/file-helpers";

interface MCPBuilderFileViewerProps {
  content: string | null;
  path: string | null;
  loading: boolean;
  onClose: () => void;
}

export function MCPBuilderFileViewer({
  content,
  path,
  loading,
  onClose,
}: MCPBuilderFileViewerProps) {
  const { t } = useTranslation("mcp");

  if (!path) {
    return null;
  }

  const ext = extOf(path);
  const lang = langFor(ext);
  const fileName = path.includes("/") ? path.slice(path.lastIndexOf("/") + 1) : path;

  return (
    <div className="flex flex-col border-t bg-muted/30 max-h-[40%]">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-3 py-1.5 text-sm">
        <div className="flex items-center gap-1.5 min-w-0">
          <FileCode2 className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate font-mono text-xs text-muted-foreground" title={path}>
            {path}
          </span>
          {lang && (
            <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
              {lang}
            </span>
          )}
        </div>
        <button
          onClick={onClose}
          className="shrink-0 rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          title={t("builder.closeFile")}
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto overscroll-contain">
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
          </div>
        ) : content !== null ? (
          <pre className="p-3 text-sm font-mono leading-relaxed whitespace-pre-wrap break-words">
            {content}
          </pre>
        ) : (
          <div className="flex flex-col items-center justify-center gap-1 py-8 text-sm text-muted-foreground">
            <FileCode2 className="h-6 w-6" />
            <span>{t("builder.fileLoadError", { name: fileName })}</span>
          </div>
        )}
      </div>
    </div>
  );
}
