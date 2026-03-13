import { useState, useEffect, useCallback, useMemo, useRef, lazy, Suspense } from "react";
import { useParams, useSearchParams, Link } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { FileTreePanel } from "@/components/shared/file-tree";
import { buildTree } from "@/lib/file-helpers";
import { ROUTES } from "@/lib/constants";
import { toast } from "@/stores/use-toast-store";
import { useManagedTools } from "./hooks/use-managed-tools";
import type { ManagedToolInfo, ManagedToolFile } from "@/types/tool";
import type { CodeLanguage } from "@/components/shared/code-editor";

const LazyCodeEditor = lazy(() =>
  import("@/components/shared/code-editor").then((m) => ({ default: m.CodeEditor })),
);

const MAX_FILE_SIZE = 500 * 1024; // 500 KB

function detectLanguage(filePath: string): CodeLanguage {
  const ext = filePath.split(".").pop()?.toLowerCase();
  switch (ext) {
    case "py":
      return "python";
    case "js":
    case "jsx":
      return "javascript";
    case "ts":
    case "tsx":
      return "typescript";
    case "json":
      return "json";
    default:
      return "text";
  }
}

export function ToolDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const { t } = useTranslation(["tools", "common"]);
  const { getManagedTool, getManagedToolFiles, getFileContent, writeFile } = useManagedTools();

  // Tool metadata
  const [tool, setTool] = useState<ManagedToolInfo | null>(null);
  const [files, setFiles] = useState<ManagedToolFile[]>([]);
  const [filesLoading, setFilesLoading] = useState(true);

  // Editor state
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [fileContent, setFileContent] = useState<string>("");
  const [savedContent, setSavedContent] = useState<string>("");
  const [contentLoading, setContentLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [fileTooLarge, setFileTooLarge] = useState(false);

  // Panel resize
  const containerRef = useRef<HTMLDivElement>(null);
  const [panelWidth, setPanelWidth] = useState(240);
  const dragging = useRef(false);

  const hasUnsaved = fileContent !== savedContent;
  const tree = useMemo(() => buildTree(files), [files]);

  // Load tool metadata + files on mount
  useEffect(() => {
    if (!id) return;
    let cancelled = false;

    async function load() {
      try {
        const [toolData, fileList] = await Promise.all([
          getManagedTool(id!),
          getManagedToolFiles(id!),
        ]);
        if (cancelled) return;
        setTool(toolData);
        setFiles(fileList);
        setFilesLoading(false);

        // Auto-select file from URL param, entry_point, or first file
        const fileParam = searchParams.get("file");
        const textFiles = fileList.filter((f: ManagedToolFile) => !f.isDir);
        let autoSelect: string | null = null;

        if (fileParam && textFiles.some((f: ManagedToolFile) => f.path === fileParam)) {
          autoSelect = fileParam;
        } else if (toolData.entry_point && textFiles.some((f: ManagedToolFile) => f.path === toolData.entry_point)) {
          autoSelect = toolData.entry_point;
        } else if (textFiles.length > 0) {
          autoSelect = textFiles[0]!.path;
        }

        if (autoSelect) {
          await loadFile(id!, autoSelect, fileList);
        }
      } catch {
        if (!cancelled) setFilesLoading(false);
      }
    }

    load();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  const loadFile = useCallback(
    async (toolId: string, path: string, fileList?: ManagedToolFile[]) => {
      // Check file size before loading
      const lookupFiles = fileList ?? files;
      const file = lookupFiles.find((f) => f.path === path);
      if (file && file.size > MAX_FILE_SIZE) {
        setSelectedFile(path);
        setFileTooLarge(true);
        setFileContent("");
        setSavedContent("");
        setSearchParams({ file: path });
        return;
      }

      setContentLoading(true);
      setFileTooLarge(false);
      try {
        const data = await getFileContent(toolId, path);
        setFileContent(data.content);
        setSavedContent(data.content);
        setSelectedFile(path);
        setSearchParams({ file: path });
      } finally {
        setContentLoading(false);
      }
    },
    [files, getFileContent, setSearchParams],
  );

  const handleSelectFile = useCallback(
    async (path: string) => {
      if (path === selectedFile) return;
      if (hasUnsaved && !window.confirm(t("tools:managed.detail.discardChanges"))) return;
      if (!id) return;
      await loadFile(id, path);
    },
    [id, selectedFile, hasUnsaved, loadFile, t],
  );

  const handleSave = useCallback(async () => {
    if (!id || !selectedFile || !hasUnsaved) return;
    setSaving(true);
    try {
      await writeFile(id, selectedFile, fileContent);
      setSavedContent(fileContent);
      toast.success(t("tools:managed.detail.fileSaved"));
    } catch {
      toast.error(t("tools:managed.detail.fileSaveFailed"));
    } finally {
      setSaving(false);
    }
  }, [id, selectedFile, fileContent, hasUnsaved, writeFile, t]);

  // Keyboard shortcut: Ctrl/Cmd+S
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "s") {
        e.preventDefault();
        handleSave();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [handleSave]);

  // beforeunload warning
  useEffect(() => {
    if (!hasUnsaved) return;
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [hasUnsaved]);

  // Panel resize handler
  const onMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      dragging.current = true;
      const startX = e.clientX;
      const startW = panelWidth;

      const onMove = (ev: MouseEvent) => {
        if (!dragging.current) return;
        const container = containerRef.current;
        if (!container) return;
        const maxW = container.offsetWidth * 0.5;
        const newW = Math.max(160, Math.min(maxW, startW + ev.clientX - startX));
        setPanelWidth(newW);
      };
      const onUp = () => {
        dragging.current = false;
        document.removeEventListener("mousemove", onMove);
        document.removeEventListener("mouseup", onUp);
      };
      document.addEventListener("mousemove", onMove);
      document.addEventListener("mouseup", onUp);
    },
    [panelWidth],
  );

  const language = selectedFile ? detectLanguage(selectedFile) : "text";

  return (
    <div className="flex h-full flex-col">
      {/* Top bar */}
      <div className="flex h-12 items-center gap-3 border-b px-4 shrink-0">
        <Link
          to={`${ROUTES.TOOLS}?tab=managed`}
          className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          <ArrowLeft className="h-4 w-4" />
          {t("tools:managed.detail.backToTools")}
        </Link>
        <span className="font-semibold truncate">{tool?.name ?? ""}</span>
        {tool?.runtime && (
          <Badge variant="outline" className="text-xs shrink-0">
            {tool.runtime}
          </Badge>
        )}
        <div className="flex-1" />
        {hasUnsaved && (
          <span className="text-xs text-amber-500 shrink-0">
            {"● "}{t("tools:managed.detail.unsaved")}
          </span>
        )}
        <Button size="sm" onClick={handleSave} disabled={saving || !hasUnsaved}>
          {saving ? t("tools:managed.detail.saving") : t("tools:managed.detail.save")}
        </Button>
      </div>

      {/* Main content: file tree + editor */}
      <div ref={containerRef} className="flex flex-1 min-h-0 overflow-hidden">
        {/* File tree panel */}
        <div
          className="shrink-0 overflow-y-auto border-r bg-muted/20 py-1"
          style={{ width: panelWidth }}
        >
          <FileTreePanel
            tree={tree}
            filesLoading={filesLoading}
            activePath={selectedFile}
            onSelect={handleSelectFile}
          />
        </div>

        {/* Resize handle */}
        <div
          className="w-1 shrink-0 cursor-col-resize bg-border hover:bg-primary/30 active:bg-primary/50 transition-colors"
          onMouseDown={onMouseDown}
        />

        {/* Editor panel */}
        <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
          {selectedFile && (
            <div className="flex items-center gap-2 border-b px-3 py-1.5 text-xs text-muted-foreground shrink-0">
              <span className="font-mono truncate">{selectedFile}</span>
            </div>
          )}

          <div className="flex-1 min-h-0 overflow-hidden">
            {contentLoading ? (
              <div className="flex items-center justify-center h-full">
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              </div>
            ) : fileTooLarge ? (
              <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
                {t("tools:managed.detail.fileTooLarge")}
              </div>
            ) : selectedFile ? (
              <Suspense
                fallback={
                  <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
                    {t("tools:managed.detail.loadingEditor")}
                  </div>
                }
              >
                <LazyCodeEditor
                  value={fileContent}
                  onChange={setFileContent}
                  language={language}
                />
              </Suspense>
            ) : (
              <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
                {t("tools:managed.detail.noFileSelected")}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
