import { useState, useEffect, useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Download, Pencil, Save, X } from "lucide-react";
import { formatSize, sizeBadgeVariant, isTextFile, type TreeNode } from "@/lib/file-helpers";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FileTreePanel } from "@/components/shared/file-tree";
import { FileContentPanel } from "@/components/shared/file-viewers";

function useIsMobile(breakpoint = 640) {
  const [mobile, setMobile] = useState(window.innerWidth < breakpoint);
  useEffect(() => {
    const onResize = () => setMobile(window.innerWidth < breakpoint);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, [breakpoint]);
  return mobile;
}

function FileActions({
  size,
  isEditing,
  saving,
  canEdit,
  onDownload,
  onEdit,
  onSave,
  onCancel,
}: {
  size: number;
  isEditing: boolean;
  saving: boolean;
  canEdit: boolean;
  onDownload?: () => void;
  onEdit?: () => void;
  onSave?: () => void;
  onCancel?: () => void;
}) {
  const { t } = useTranslation("storage");
  const { t: tc } = useTranslation("common");

  if (isEditing) {
    return (
      <div className="flex items-center gap-1.5 shrink-0 ml-auto">
        <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={onCancel} disabled={saving}>
          <X className="h-3 w-3 mr-1" />
          {tc("cancel", "Cancel")}
        </Button>
        <Button variant="default" size="sm" className="h-6 px-2 text-xs" onClick={onSave} disabled={saving}>
          <Save className="h-3 w-3 mr-1" />
          {saving ? t("edit.saving") : t("edit.save")}
        </Button>
      </div>
    );
  }

  return (
    <div className="flex items-center gap-1.5 shrink-0 ml-auto">
      <Badge variant={sizeBadgeVariant(size)} className="text-[10px] px-1.5 py-0">
        {formatSize(size)}
      </Badge>
      {canEdit && onEdit && (
        <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onEdit} title={t("edit.title")}>
          <Pencil className="h-3.5 w-3.5" />
        </Button>
      )}
      {onDownload && (
        <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onDownload} title={tc("download")}>
          <Download className="h-3.5 w-3.5" />
        </Button>
      )}
    </div>
  );
}

export function FileBrowser({
  tree,
  filesLoading,
  activePath,
  onSelect,
  contentLoading,
  fileContent,
  onDelete,
  onLoadMore,
  onMove,
  onDownload,
  onSave,
  fetchBlob,
  showSize,
}: {
  tree: TreeNode[];
  filesLoading: boolean;
  activePath: string | null;
  onSelect: (path: string) => void;
  contentLoading: boolean;
  fileContent: { content: string; path: string; size: number } | null;
  onDelete?: (path: string, isDir: boolean) => void;
  onLoadMore?: (path: string) => void;
  onMove?: (fromPath: string, toFolder: string) => void;
  onDownload?: (path: string) => void;
  onSave?: (path: string, content: string) => Promise<void>;
  fetchBlob?: (path: string) => Promise<Blob>;
  showSize?: boolean;
}) {
  const isMobile = useIsMobile();
  const { t } = useTranslation("common");
  const containerRef = useRef<HTMLDivElement>(null);
  const [treeWidth, setTreeWidth] = useState(220);
  const [mobileShowTree, setMobileShowTree] = useState(true);
  const dragging = useRef(false);

  const [isEditing, setIsEditing] = useState(false);
  const [editContent, setEditContent] = useState("");
  const [saving, setSaving] = useState(false);

  // Reset edit mode whenever the selected file changes.
  useEffect(() => {
    setIsEditing(false);
    setEditContent("");
    setSaving(false);
  }, [fileContent?.path]);

  const canEdit = !!(onSave && fileContent && isTextFile(fileContent.path) && !contentLoading);

  const handleEdit = useCallback(() => {
    if (!fileContent) return;
    setEditContent(fileContent.content);
    setIsEditing(true);
  }, [fileContent]);

  const handleCancel = useCallback(() => {
    setIsEditing(false);
    setEditContent("");
  }, []);

  const handleSave = useCallback(async () => {
    if (!onSave || !fileContent) return;
    setSaving(true);
    try {
      await onSave(fileContent.path, editContent);
      setIsEditing(false);
    } catch {
      // toast is shown inside onSave
    } finally {
      setSaving(false);
    }
  }, [onSave, fileContent, editContent]);

  const handleSelect = useCallback((path: string) => {
    setIsEditing(false);
    setEditContent("");
    onSelect(path);
    if (isMobile) setMobileShowTree(false);
  }, [onSelect, isMobile]);

  const onMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    dragging.current = true;
    const startX = e.clientX;
    const startW = treeWidth;

    const onMove = (ev: MouseEvent) => {
      if (!dragging.current) return;
      const container = containerRef.current;
      if (!container) return;
      const maxW = container.offsetWidth * 0.5;
      const newW = Math.max(140, Math.min(maxW, startW + ev.clientX - startX));
      setTreeWidth(newW);
    };
    const onUp = () => {
      dragging.current = false;
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  }, [treeWidth]);

  // Mobile: stacked layout
  if (isMobile) {
    return (
      <div className="flex-1 flex flex-col border rounded-md overflow-hidden min-h-0">
        {mobileShowTree ? (
          <div className="flex-1 overflow-y-auto bg-muted/20 py-1">
            <FileTreePanel tree={tree} filesLoading={filesLoading} activePath={activePath} onSelect={handleSelect} onDelete={onDelete} onLoadMore={onLoadMore} onMove={onMove} showSize={showSize} />
          </div>
        ) : (
          <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
            <div className="flex items-center gap-2 text-xs text-muted-foreground border-b px-3 py-2 shrink-0">
              <button
                type="button"
                onClick={() => setMobileShowTree(true)}
                className="text-primary hover:underline cursor-pointer shrink-0"
              >
                &larr; {t("filesBack")}
              </button>
              {fileContent && (
                <>
                  <span className="font-mono truncate">{fileContent.path}</span>
                  <FileActions
                    size={fileContent.size}
                    isEditing={isEditing}
                    saving={saving}
                    canEdit={canEdit}
                    onDownload={onDownload ? () => onDownload(fileContent.path) : undefined}
                    onEdit={handleEdit}
                    onSave={handleSave}
                    onCancel={handleCancel}
                  />
                </>
              )}
            </div>
            <div className="flex-1 overflow-auto p-3 min-h-0">
              {isEditing ? (
                <textarea
                  className="w-full h-full min-h-[400px] resize-none rounded-md border bg-muted/30 p-3 text-sm font-mono focus:outline-none focus:ring-1 focus:ring-ring"
                  value={editContent}
                  onChange={(e) => setEditContent(e.target.value)}
                  disabled={saving}
                  spellCheck={false}
                  autoComplete="off"
                  autoCorrect="off"
                  autoCapitalize="off"
                />
              ) : (
                <FileContentPanel fileContent={fileContent} contentLoading={contentLoading} fetchBlob={fetchBlob} onDownload={onDownload} />
              )}
            </div>
          </div>
        )}
      </div>
    );
  }

  // Desktop: side-by-side with resizable divider
  return (
    <div ref={containerRef} className="flex-1 flex border rounded-md overflow-hidden min-h-0">
      <div className="overflow-y-auto bg-muted/20 py-1 shrink-0" style={{ width: treeWidth }}>
        <FileTreePanel tree={tree} filesLoading={filesLoading} activePath={activePath} onSelect={handleSelect} onDelete={onDelete} onLoadMore={onLoadMore} onMove={onMove} showSize={showSize} />
      </div>

      <div
        className="w-1 cursor-col-resize bg-border hover:bg-primary/30 active:bg-primary/50 shrink-0"
        onMouseDown={onMouseDown}
      />

      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        {fileContent && (
          <div className="flex items-center justify-between text-xs text-muted-foreground border-b px-3 py-2 shrink-0">
            <span className="font-mono truncate">{fileContent.path}</span>
            <FileActions
              size={fileContent.size}
              isEditing={isEditing}
              saving={saving}
              canEdit={canEdit}
              onDownload={onDownload ? () => onDownload(fileContent.path) : undefined}
              onEdit={handleEdit}
              onSave={handleSave}
              onCancel={handleCancel}
            />
          </div>
        )}
        <div className="flex-1 overflow-auto p-3 min-h-0">
          {isEditing ? (
            <textarea
              className="w-full h-full min-h-[400px] resize-none rounded-md border bg-muted/30 p-3 text-sm font-mono focus:outline-none focus:ring-1 focus:ring-ring"
              value={editContent}
              onChange={(e) => setEditContent(e.target.value)}
              disabled={saving}
              spellCheck={false}
              autoComplete="off"
              autoCorrect="off"
              autoCapitalize="off"
            />
          ) : (
            <FileContentPanel fileContent={fileContent} contentLoading={contentLoading} fetchBlob={fetchBlob} onDownload={onDownload} />
          )}
        </div>
      </div>
    </div>
  );
}
