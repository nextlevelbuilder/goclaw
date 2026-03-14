import { useState, useEffect, useCallback, useMemo } from "react";
import { Info, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { PageHeader } from "@/components/shared/page-header";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { buildTree, mergeSubtree, setNodeLoading, formatSize, isTextFile } from "@/lib/file-helpers";
import { FileBrowser } from "@/components/shared/file-browser";
import { useStorage, useStorageSize } from "./hooks/use-storage";

export function StoragePage() {
  const { t } = useTranslation("storage");
  const { files, baseDir, loading, listFiles, loadSubtree, readFile, deleteFile, fetchRawBlob } = useStorage();
  const { totalSize, loading: sizeLoading, refreshSize } = useStorageSize();

  const [tree, setTree] = useState(buildTree(files));
  const [activePath, setActivePath] = useState<string | null>(null);
  const [fileContent, setFileContent] = useState<{ content: string; path: string; size: number } | null>(null);
  const [contentLoading, setContentLoading] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ path: string; isDir: boolean } | null>(null);
  const [deleting, setDeleting] = useState(false);

  // Rebuild tree when files change from initial load or refresh
  useEffect(() => { setTree(buildTree(files)); }, [files]);

  // Load file list + size on mount
  useEffect(() => { listFiles(); refreshSize(); }, [listFiles, refreshSize]);

  const handleLoadMore = useCallback(async (path: string) => {
    // Mark node as loading
    setTree((prev) => setNodeLoading(prev, path, true));
    try {
      const children = await loadSubtree(path);
      setTree((prev) => mergeSubtree(prev, path, children));
    } catch {
      setTree((prev) => setNodeLoading(prev, path, false));
    }
  }, [loadSubtree]);

  /** Find a file node's size from the flat files list. */
  const fileSizeMap = useMemo(() => {
    const m = new Map<string, number>();
    for (const f of files) if (!f.isDir) m.set(f.path, f.size);
    return m;
  }, [files]);

  const handleSelect = useCallback(async (path: string) => {
    setActivePath(path);
    if (isTextFile(path)) {
      setContentLoading(true);
      try {
        const res = await readFile(path);
        setFileContent(res);
      } catch {
        setFileContent(null);
      } finally {
        setContentLoading(false);
      }
    } else {
      // For non-text files (images, binaries): don't fetch content — just set metadata.
      // ImageViewer will fetch the blob separately; UnsupportedViewer just shows size.
      const size = fileSizeMap.get(path) ?? 0;
      setFileContent({ content: "", path, size });
    }
  }, [readFile, fileSizeMap]);

  const handleDeleteRequest = useCallback((path: string, isDir: boolean) => {
    setDeleteTarget({ path, isDir });
  }, []);

  const handleDeleteConfirm = useCallback(async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await deleteFile(deleteTarget.path);
      if (activePath === deleteTarget.path || (deleteTarget.isDir && activePath?.startsWith(deleteTarget.path + "/"))) {
        setActivePath(null);
        setFileContent(null);
      }
      await listFiles();
    } finally {
      setDeleting(false);
      setDeleteTarget(null);
    }
  }, [deleteTarget, deleteFile, listFiles, activePath]);

  const handleDownload = useCallback(async (path: string) => {
    try {
      const blob = await fetchRawBlob(path, true);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = path.split("/").pop() ?? "download";
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch { /* silent fail */ }
  }, [fetchRawBlob]);

  /** Fetch raw blob for image rendering (no download header). */
  const handleFetchBlob = useCallback(async (path: string) => {
    return fetchRawBlob(path, false);
  }, [fetchRawBlob]);

  const handleRefresh = useCallback(() => {
    listFiles();
    refreshSize();
  }, [listFiles, refreshSize]);

  const deleteName = deleteTarget?.path.split("/").pop() ?? "";

  // Size description with cache tooltip
  const sizeDescription = useMemo(() => {
    if (!baseDir) return t("description");
    const sizeStr = sizeLoading ? `${formatSize(totalSize)}...` : formatSize(totalSize);
    return t("descriptionWithPath", { path: baseDir, size: sizeStr });
  }, [baseDir, totalSize, sizeLoading, t]);

  return (
    <div className="flex flex-col h-full p-4 sm:p-6">
      <PageHeader
        title={t("title")}
        description={
          <span className="inline-flex items-center gap-1">
            {sizeDescription}
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-3.5 w-3.5 text-muted-foreground/60 cursor-help shrink-0" />
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  <p>{t("sizeCacheInfo")}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </span>
        }
        actions={
          <Button variant="outline" size="sm" onClick={handleRefresh} disabled={loading}>
            <RefreshCw className={`h-4 w-4 mr-1.5 ${loading ? "animate-spin" : ""}`} />
            {t("common:refresh", "Refresh")}
          </Button>
        }
      />

      <div className="mt-4 flex-1 flex flex-col min-h-0">
        <FileBrowser
          tree={tree}
          filesLoading={loading}
          activePath={activePath}
          onSelect={handleSelect}
          contentLoading={contentLoading}
          fileContent={fileContent}
          onDelete={handleDeleteRequest}
          onLoadMore={handleLoadMore}
          onDownload={handleDownload}
          fetchBlob={handleFetchBlob}
          showSize
        />
      </div>

      {/* Delete confirmation dialog */}
      <Dialog open={!!deleteTarget} onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{deleteTarget?.isDir ? t("delete.folderTitle") : t("delete.fileTitle")}</DialogTitle>
            <DialogDescription>
              {t("delete.description", { name: deleteName })}
              {deleteTarget?.isDir && t("delete.folderWarning")}
              {t("delete.undone")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleting}>
              {t("common:cancel", "Cancel")}
            </Button>
            <Button variant="destructive" onClick={handleDeleteConfirm} disabled={deleting}>
              {deleting ? t("delete.deleting") : t("delete.confirmLabel")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
