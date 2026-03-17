import { useState, useEffect, useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Download } from "lucide-react";
import { formatSize, sizeBadgeVariant, type TreeNode } from "@/lib/file-helpers";
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

/** File size badge + download button row. */
function FileActions({
  size,
  onDownload,
}: {
  size: number;
  onDownload?: () => void;
}) {
  const { t } = useTranslation("common");
  return (
    <div className="flex items-center gap-1.5 shrink-0 ml-auto">
      <Badge variant={sizeBadgeVariant(size)} className="text-[10px] px-1.5 py-0">
        {formatSize(size)}
      </Badge>
      {onDownload && (
        <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onDownload} title={t("download")}>
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
  onDownload,
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
  onDownload?: (path: string) => void;
  fetchBlob?: (path: string) => Promise<Blob>;
  showSize?: boolean;
}) {
  const isMobile = useIsMobile();
  const { t } = useTranslation("common");
  const containerRef = useRef<HTMLDivElement>(null);
  const [treeWidth, setTreeWidth] = useState(220);
  const [mobileShowTree, setMobileShowTree] = useState(true);
  const dragging = useRef(false);

  const handleSelect = useCallback((path: string) => {
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

  // Mobile: stacked layout — tree first, tap file -> content view
  if (isMobile) {
    return (
      <div className="flex-1 flex flex-col border rounded-md overflow-hidden min-h-0">
        {mobileShowTree ? (
          <div className="flex-1 overflow-y-auto bg-muted/20 py-1">
            <FileTreePanel tree={tree} filesLoading={filesLoading} activePath={activePath} onSelect={handleSelect} onDelete={onDelete} onLoadMore={onLoadMore} showSize={showSize} />
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
                  <FileActions size={fileContent.size} onDownload={onDownload ? () => onDownload(fileContent.path) : undefined} />
                </>
              )}
            </div>
            <div className="flex-1 overflow-auto p-3 min-h-0">
              <FileContentPanel fileContent={fileContent} contentLoading={contentLoading} fetchBlob={fetchBlob} onDownload={onDownload} />
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
        <FileTreePanel tree={tree} filesLoading={filesLoading} activePath={activePath} onSelect={handleSelect} onDelete={onDelete} onLoadMore={onLoadMore} showSize={showSize} />
      </div>

      <div
        className="w-1 cursor-col-resize bg-border hover:bg-primary/30 active:bg-primary/50 shrink-0"
        onMouseDown={onMouseDown}
      />

      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        {fileContent && (
          <div className="flex items-center justify-between text-xs text-muted-foreground border-b px-3 py-2 shrink-0">
            <span className="font-mono truncate">{fileContent.path}</span>
            <FileActions size={fileContent.size} onDownload={onDownload ? () => onDownload(fileContent.path) : undefined} />
          </div>
        )}
        <div className="flex-1 overflow-auto p-3 min-h-0">
          <FileContentPanel fileContent={fileContent} contentLoading={contentLoading} fetchBlob={fetchBlob} onDownload={onDownload} />
        </div>
      </div>
    </div>
  );
}
