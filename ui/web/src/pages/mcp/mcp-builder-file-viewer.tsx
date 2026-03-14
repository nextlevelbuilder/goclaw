import { useEffect, useRef, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { X, FileCode2 } from "lucide-react";
import { extOf, langFor } from "@/lib/file-helpers";
import { EditorView } from "@codemirror/view";
import { EditorState } from "@codemirror/state";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { css } from "@codemirror/lang-css";
import { html } from "@codemirror/lang-html";
import { python } from "@codemirror/lang-python";
import { syntaxHighlighting, defaultHighlightStyle } from "@codemirror/language";
import { oneDark } from "@codemirror/theme-one-dark";
import type { Extension } from "@codemirror/state";

function getLangExtension(ext: string): Extension | null {
  switch (ext) {
    case "ts":
    case "tsx":
      return javascript({ jsx: ext === "tsx", typescript: true });
    case "js":
    case "jsx":
      return javascript({ jsx: ext === "jsx" });
    case "json":
      return json();
    case "md":
      return markdown();
    case "css":
      return css();
    case "html":
      return html();
    case "py":
      return python();
    default:
      return null;
  }
}

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
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);

  const ext = path ? extOf(path) : "";
  const lang = path ? langFor(ext) : "";
  const fileName = path?.includes("/") ? path.slice(path.lastIndexOf("/") + 1) : path;

  const isDark = useMemo(() => {
    return document.documentElement.classList.contains("dark");
  }, [content, path]);

  useEffect(() => {
    if (!editorRef.current || content === null) return;

    const langExt = getLangExtension(ext);

    const extensions: Extension[] = [
      EditorView.editable.of(false),
      EditorState.readOnly.of(true),
      EditorView.lineWrapping,
      syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
      EditorView.theme({
        "&": { fontSize: "13px", maxHeight: "100%", backgroundColor: "transparent" },
        ".cm-gutters": { backgroundColor: "transparent", border: "none" },
        ".cm-scroller": { overflow: "auto", fontFamily: "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, monospace" },
        ".cm-content": { padding: "8px 0" },
      }),
    ];

    if (isDark) extensions.push(oneDark);
    if (langExt) extensions.push(langExt);

    const state = EditorState.create({ doc: content, extensions });

    if (viewRef.current) {
      viewRef.current.destroy();
    }

    viewRef.current = new EditorView({ state, parent: editorRef.current });

    return () => {
      viewRef.current?.destroy();
      viewRef.current = null;
    };
  }, [content, ext, isDark]);

  if (!path) return null;

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
          <div ref={editorRef} className="min-h-0" />
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
