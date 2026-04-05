import { useState, useEffect, useCallback, useMemo } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import {
  DndContext,
  DragOverlay,
  useDroppable,
  useDraggable,
} from "@dnd-kit/core";
import {
  Folder,
  FolderOpen,
  FileText,
  FileCode2,
  File,
  FileImage,
  FileJson2,
  FileSpreadsheet,
  FileTerminal,
  FileArchive,
  FileVideo,
  FileAudio,
  FileCog,
  FileType,
  FileLock,
  ChevronRight,
  Loader2,
  Trash2,
} from "lucide-react";
import { extOf, CODE_EXTENSIONS, IMAGE_EXTENSIONS, formatSize, type TreeNode } from "@/lib/file-helpers";
import { useTreeDnd } from "@/hooks/use-tree-dnd";
import { DragPreview } from "@/components/shared/drag-preview";

const cls = "h-4 w-4 shrink-0";

function FileIcon({ name }: { name: string }) {
  const ext = extOf(name);
  if (ext === "md" || ext === "mdx") return <FileText className={`${cls} text-blue-500`} />;
  if (ext === "json" || ext === "json5") return <FileJson2 className={`${cls} text-yellow-600`} />;
  if (ext === "yaml" || ext === "yml" || ext === "toml") return <FileCog className={`${cls} text-orange-500`} />;
  if (ext === "csv") return <FileSpreadsheet className={`${cls} text-green-600`} />;
  if (ext === "sh" || ext === "bash" || ext === "zsh") return <FileTerminal className={`${cls} text-lime-600`} />;
  if (IMAGE_EXTENSIONS.has(ext)) return <FileImage className={`${cls} text-emerald-500`} />;
  if (ext === "mp4" || ext === "webm" || ext === "mov" || ext === "avi" || ext === "mkv") return <FileVideo className={`${cls} text-pink-500`} />;
  if (ext === "mp3" || ext === "wav" || ext === "ogg" || ext === "flac" || ext === "m4a") return <FileAudio className={`${cls} text-orange-500`} />;
  if (ext === "zip" || ext === "tar" || ext === "gz" || ext === "rar" || ext === "7z" || ext === "bz2") return <FileArchive className={`${cls} text-amber-600`} />;
  if (ext === "ttf" || ext === "otf" || ext === "woff" || ext === "woff2") return <FileType className={`${cls} text-slate-500`} />;
  if (ext === "env" || ext === "pem" || ext === "key" || ext === "crt") return <FileLock className={`${cls} text-red-500`} />;
  if (CODE_EXTENSIONS.has(ext)) return <FileCode2 className={`${cls} text-orange-500`} />;
  return <File className={`${cls} text-muted-foreground`} />;
}

/** Draggable wrapper for tree items (files and folders). */
function DraggableItem({
  id,
  enabled,
  children,
}: {
  id: string;
  enabled: boolean;
  children: React.ReactNode;
}) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id,
    disabled: !enabled,
  });

  return (
    <div
      ref={setNodeRef}
      {...(enabled ? { ...listeners, ...attributes } : {})}
      className={isDragging ? "opacity-40" : ""}
    >
      {children}
    </div>
  );
}

/** Droppable wrapper for folder items. */
function DroppableFolder({
  id,
  enabled,
  children,
}: {
  id: string;
  enabled: boolean;
  children: (props: { isDropTarget: boolean }) => React.ReactNode;
}) {
  const { setNodeRef, isOver } = useDroppable({
    id,
    disabled: !enabled,
  });

  return (
    <div ref={setNodeRef}>
      {children({ isDropTarget: isOver })}
    </div>
  );
}

export function TreeItem({
  node,
  depth,
  activePath,
  onSelect,
  onDelete,
  onLoadMore,
  dndEnabled,
  autoExpandPath,
  showSize,
}: {
  node: TreeNode;
  depth: number;
  activePath: string | null;
  onSelect: (path: string) => void;
  onDelete?: (path: string, isDir: boolean) => void;
  onLoadMore?: (path: string) => void;
  dndEnabled: boolean;
  autoExpandPath: string | null;
  showSize?: boolean;
}) {
  const { t } = useTranslation("common");
  const [expanded, setExpanded] = useState(depth === 0);
  const isActive = activePath === node.path;

  // Auto-expand folder when hovered during drag for 800ms.
  useEffect(() => {
    if (autoExpandPath === node.path && node.isDir && !expanded) {
      setExpanded(true);
      if (node.hasChildren && node.children.length === 0 && !node.loading) {
        onLoadMore?.(node.path);
      }
    }
  }, [autoExpandPath, node.path, node.isDir, expanded, node.hasChildren, node.children.length, node.loading, onLoadMore]);

  const handleToggle = useCallback(() => {
    const willExpand = !expanded;
    setExpanded(willExpand);
    if (willExpand && node.isDir && node.hasChildren && node.children.length === 0 && !node.loading) {
      onLoadMore?.(node.path);
    }
  }, [expanded, node.isDir, node.hasChildren, node.children.length, node.loading, node.path, onLoadMore]);

  const deleteBtn = onDelete && !node.protected && (
    <button
      type="button"
      className="ml-auto shrink-0 opacity-0 group-hover/tree-item:opacity-100 text-destructive hover:text-destructive/80 transition-opacity cursor-pointer p-0.5"
      title={node.isDir ? t("deleteFolder") : t("deleteFile")}
      onClick={(e) => { e.stopPropagation(); onDelete(node.path, node.isDir); }}
    >
      <Trash2 className="h-3.5 w-3.5" />
    </button>
  );

  const sizeLabel = showSize && (node.isDir ? 0 : node.size) > 0 && (
    <span className="ml-auto shrink-0 text-[10px] text-muted-foreground tabular-nums">
      {formatSize(node.size)}
    </span>
  );

  if (node.isDir) {
    const folderContent = (isDropTargetActive: boolean) => (
      <>
        <div
          className={`group/tree-item flex w-full items-center gap-1 rounded px-2 py-1 text-left text-sm cursor-pointer ${
            isDropTargetActive ? "bg-primary/10 ring-1 ring-primary" : "hover:bg-accent"
          }`}
          style={{ paddingLeft: `${depth * 16 + 8}px` }}
          onClick={handleToggle}
        >
          <ChevronRight
            className={`h-3 w-3 shrink-0 transition-transform ${expanded ? "rotate-90" : ""}`}
          />
          {expanded ? (
            <FolderOpen className="h-4 w-4 shrink-0 text-yellow-600" />
          ) : (
            <Folder className="h-4 w-4 shrink-0 text-yellow-600" />
          )}
          <span className="truncate">{node.name}</span>
          {node.loading && <Loader2 className="h-3 w-3 shrink-0 animate-spin text-muted-foreground ml-1" />}
          {sizeLabel}
          {deleteBtn}
        </div>
        {expanded && node.children.map((child) => (
          <TreeItem
            key={child.path}
            node={child}
            depth={depth + 1}
            activePath={activePath}
            onSelect={onSelect}
            onDelete={onDelete}
            onLoadMore={onLoadMore}
            dndEnabled={dndEnabled}

            autoExpandPath={autoExpandPath}
            showSize={showSize}
          />
        ))}
        {expanded && node.hasChildren && node.children.length === 0 && !node.loading && (
          <div
            className="flex items-center gap-1 text-xs text-muted-foreground cursor-pointer hover:text-foreground"
            style={{ paddingLeft: `${(depth + 1) * 16 + 20}px` }}
            onClick={() => onLoadMore?.(node.path)}
          >
            <Loader2 className="h-3 w-3" />
            <span>{t("loadMore")}</span>
          </div>
        )}
      </>
    );

    if (dndEnabled) {
      return (
        <DraggableItem id={node.path} enabled>
          <DroppableFolder id={node.path} enabled>
            {({ isDropTarget: active }) => folderContent(active)}
          </DroppableFolder>
        </DraggableItem>
      );
    }

    return <div>{folderContent(false)}</div>;
  }

  // File node
  const fileContent = (
    <div
      className={`group/tree-item flex w-full items-center gap-1.5 rounded px-2 py-1 text-left text-sm cursor-pointer ${
        isActive ? "bg-accent text-accent-foreground" : "hover:bg-accent/50"
      }`}
      style={{ paddingLeft: `${depth * 16 + 20}px` }}
      onClick={() => onSelect(node.path)}
    >
      <FileIcon name={node.name} />
      <span className="truncate">{node.name}</span>
      {sizeLabel}
      {deleteBtn}
    </div>
  );

  if (dndEnabled) {
    return (
      <DraggableItem id={node.path} enabled>
        {fileContent}
      </DraggableItem>
    );
  }

  return fileContent;
}

/** Find a node by path in the tree. */
function findNode(tree: TreeNode[], path: string): TreeNode | undefined {
  for (const node of tree) {
    if (node.path === path) return node;
    if (node.children.length > 0) {
      const found = findNode(node.children, path);
      if (found) return found;
    }
  }
  return undefined;
}

export function FileTreePanel({
  tree,
  filesLoading,
  activePath,
  onSelect,
  onDelete,
  onLoadMore,
  onMove,
  showSize,
}: {
  tree: TreeNode[];
  filesLoading: boolean;
  activePath: string | null;
  onSelect: (path: string) => void;
  onDelete?: (path: string, isDir: boolean) => void;
  onLoadMore?: (path: string) => void;
  onMove?: (fromPath: string, toFolder: string) => void;
  showSize?: boolean;
}) {
  const { t } = useTranslation("common");
  const { sensors, activeId, autoExpandPath, handlers } = useTreeDnd(onMove);
  const dndEnabled = !!onMove;

  // Find the active node for DragOverlay preview.
  const activeNode = useMemo(
    () => (activeId ? findNode(tree, activeId) : undefined),
    [activeId, tree],
  );

  if (filesLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }
  if (tree.length === 0) {
    return <p className="px-3 py-4 text-sm text-muted-foreground">{t("noFiles")}</p>;
  }

  const treeContent = (
    <div className="flex-1 min-h-0">
      {/* Root-level drop target for moving to root */}
      {dndEnabled ? (
        <RootDropZone>
          {tree.map((node) => (
            <TreeItem
              key={node.path} node={node} depth={0} activePath={activePath}
              onSelect={onSelect} onDelete={onDelete} onLoadMore={onLoadMore}
              dndEnabled={dndEnabled} autoExpandPath={autoExpandPath}
              showSize={showSize}
            />
          ))}
        </RootDropZone>
      ) : (
        tree.map((node) => (
          <TreeItem
            key={node.path} node={node} depth={0} activePath={activePath}
            onSelect={onSelect} onDelete={onDelete} onLoadMore={onLoadMore}
            dndEnabled={false} autoExpandPath={null}
            showSize={showSize}
          />
        ))
      )}
    </div>
  );

  if (!dndEnabled) return treeContent;

  return (
    <DndContext sensors={sensors} {...handlers}>
      {treeContent}
      {/* Portal to document.body so DragOverlay isn't offset by Radix Dialog's CSS transform. */}
      {createPortal(
        <DragOverlay dropAnimation={null}>
          {activeNode ? (
            <DragPreview name={activeNode.name} isDir={activeNode.isDir} />
          ) : null}
        </DragOverlay>,
        document.body,
      )}
    </DndContext>
  );
}

/** Root-level droppable zone — dropping here moves to root (""). */
function RootDropZone({ children }: { children: React.ReactNode }) {
  const { setNodeRef, isOver } = useDroppable({ id: "__root__" });

  return (
    <div ref={setNodeRef} className={`min-h-full ${isOver ? "bg-primary/5" : ""}`}>
      {children}
    </div>
  );
}
